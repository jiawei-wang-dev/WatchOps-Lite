package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const maxPrometheusResponseBytes = 1 << 20

type PrometheusClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewPrometheusClient(baseURL string, timeout time.Duration) (*PrometheusClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("%w: parse base URL: %v", ErrInvalidArgument, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: Prometheus base URL must be an HTTP(S) origin without credentials", ErrInvalidArgument)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("%w: request timeout must be greater than zero", ErrInvalidArgument)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &PrometheusClient{
		baseURL:    parsed.String(),
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (c *PrometheusClient) Query(
	ctx context.Context,
	expression string,
	at time.Time,
) (samples []Sample, resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"prometheus.query",
		attribute.String("prometheus.base_url", c.baseURL),
		attribute.Int("query_length", len(expression)),
	)
	defer func() {
		span.SetAttributes(attribute.Int("result_count", len(samples)))
		if resultErr != nil {
			observability.MarkError(span, "Prometheus query failed")
		}
		span.End()
	}()

	endpoint, err := url.Parse(c.baseURL + "/api/v1/query")
	if err != nil {
		return nil, fmt.Errorf("%w: build query endpoint", ErrUnavailable)
	}
	parameters := endpoint.Query()
	parameters.Set("query", expression)
	parameters.Set("time", strconv.FormatInt(at.Unix(), 10))
	endpoint.RawQuery = parameters.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request", ErrUnavailable)
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: query request failed: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("%w: Prometheus returned HTTP %d", ErrUnavailable, response.StatusCode)
	}

	var payload prometheusResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxPrometheusResponseBytes))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrUnavailable, err)
	}
	if payload.Status != "success" {
		return nil, fmt.Errorf("%w: Prometheus query status was %q", ErrUnavailable, payload.Status)
	}
	return parsePrometheusData(payload.Data, expression)
}

type prometheusResponse struct {
	Status string         `json:"status"`
	Data   prometheusData `json:"data"`
}

type prometheusData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

type prometheusVectorSample struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}

func parsePrometheusData(data prometheusData, expression string) ([]Sample, error) {
	if data.ResultType != "vector" {
		return nil, fmt.Errorf("%w: unsupported Prometheus result type %q", ErrUnavailable, data.ResultType)
	}
	var result []prometheusVectorSample
	if err := json.Unmarshal(data.Result, &result); err != nil {
		return nil, fmt.Errorf("%w: decode vector result: %v", ErrUnavailable, err)
	}
	samples := make([]Sample, 0, len(result))
	for _, item := range result {
		timestamp, value, err := parsePrometheusValue(item.Value)
		if err != nil {
			return nil, err
		}
		labels := make(map[string]string, len(item.Metric))
		for name, labelValue := range item.Metric {
			if name != "__name__" {
				labels[name] = labelValue
			}
		}
		samples = append(samples, Sample{
			Name:      item.Metric["__name__"],
			Value:     value,
			Timestamp: timestamp,
			Service:   item.Metric["service"],
			Labels:    labels,
			Query:     expression,
		})
	}
	return samples, nil
}

func parsePrometheusValue(value []json.RawMessage) (time.Time, float64, error) {
	if len(value) != 2 {
		return time.Time{}, 0, fmt.Errorf("%w: metric sample must contain timestamp and value", ErrUnavailable)
	}
	var timestampSeconds float64
	if err := json.Unmarshal(value[0], &timestampSeconds); err != nil {
		return time.Time{}, 0, fmt.Errorf("%w: invalid sample timestamp", ErrUnavailable)
	}
	var encodedValue string
	if err := json.Unmarshal(value[1], &encodedValue); err != nil {
		return time.Time{}, 0, fmt.Errorf("%w: invalid sample value", ErrUnavailable)
	}
	parsedValue, err := strconv.ParseFloat(encodedValue, 64)
	if err != nil || math.IsNaN(parsedValue) || math.IsInf(parsedValue, 0) {
		return time.Time{}, 0, fmt.Errorf("%w: invalid finite sample value", ErrUnavailable)
	}
	seconds, fraction := math.Modf(timestampSeconds)
	timestamp := time.Unix(int64(seconds), int64(fraction*float64(time.Second))).UTC()
	return timestamp, parsedValue, nil
}
