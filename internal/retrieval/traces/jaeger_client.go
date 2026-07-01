package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const maxJaegerResponseBytes = 4 << 20

type JaegerClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewJaegerClient(baseURL string, timeout time.Duration) (*JaegerClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("%w: parse base URL: %v", ErrInvalidArgument, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return nil, fmt.Errorf(
			"%w: Jaeger base URL must be an HTTP(S) origin without credentials",
			ErrInvalidArgument,
		)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("%w: request timeout must be greater than zero", ErrInvalidArgument)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &JaegerClient{
		baseURL:    parsed.String(),
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (c *JaegerClient) Search(
	ctx context.Context,
	query Query,
) (spans []Span, resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"jaeger.query",
		attribute.String("jaeger.base_url", c.baseURL),
		attribute.String("service", query.Service),
		attribute.String("operation", query.Operation),
		attribute.String("trace_id", query.TraceID),
	)
	defer func() {
		span.SetAttributes(attribute.Int("result_count", len(spans)))
		if resultErr != nil {
			observability.MarkError(span, "Jaeger query failed")
		}
		span.End()
	}()

	endpoint, err := c.queryURL(query)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request", ErrUnavailable)
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: query request failed: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return []Span{}, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("%w: Jaeger returned HTTP %d", ErrUnavailable, response.StatusCode)
	}

	var payload jaegerResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxJaegerResponseBytes))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrUnavailable, err)
	}
	return parseJaegerResponse(payload), nil
}

func (c *JaegerClient) queryURL(query Query) (string, error) {
	if query.TraceID != "" {
		return c.baseURL + "/api/traces/" + url.PathEscape(query.TraceID), nil
	}
	endpoint, err := url.Parse(c.baseURL + "/api/traces")
	if err != nil {
		return "", fmt.Errorf("%w: build query endpoint", ErrUnavailable)
	}
	parameters := endpoint.Query()
	parameters.Set("service", query.Service)
	parameters.Set("limit", strconv.Itoa(query.Limit))
	parameters.Set("start", strconv.FormatInt(query.From.UnixMicro(), 10))
	parameters.Set("end", strconv.FormatInt(query.To.UnixMicro(), 10))
	if query.Operation != "" {
		parameters.Set("operation", query.Operation)
	}
	endpoint.RawQuery = parameters.Encode()
	return endpoint.String(), nil
}

type jaegerResponse struct {
	Data []jaegerTrace `json:"data"`
}

type jaegerTrace struct {
	TraceID   string                   `json:"traceID"`
	Spans     []jaegerSpan             `json:"spans"`
	Processes map[string]jaegerProcess `json:"processes"`
}

type jaegerSpan struct {
	TraceID       string            `json:"traceID"`
	SpanID        string            `json:"spanID"`
	OperationName string            `json:"operationName"`
	References    []jaegerReference `json:"references"`
	StartTime     int64             `json:"startTime"`
	Duration      int64             `json:"duration"`
	Tags          []jaegerTag       `json:"tags"`
	ProcessID     string            `json:"processID"`
}

type jaegerReference struct {
	RefType string `json:"refType"`
	SpanID  string `json:"spanID"`
}

type jaegerTag struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type jaegerProcess struct {
	ServiceName string `json:"serviceName"`
}

func parseJaegerResponse(payload jaegerResponse) []Span {
	result := make([]Span, 0)
	for _, trace := range payload.Data {
		for _, jaegerSpan := range trace.Spans {
			tags := make(map[string]any, len(jaegerSpan.Tags))
			errorSpan := false
			for _, tag := range jaegerSpan.Tags {
				tags[tag.Key] = tag.Value
				if tagMarksError(tag) {
					errorSpan = true
				}
			}
			processService := trace.Processes[jaegerSpan.ProcessID].ServiceName
			traceID := jaegerSpan.TraceID
			if traceID == "" {
				traceID = trace.TraceID
			}
			result = append(result, Span{
				TraceID:        traceID,
				SpanID:         jaegerSpan.SpanID,
				ParentSpanID:   parentSpanID(jaegerSpan.References),
				Service:        processService,
				Operation:      jaegerSpan.OperationName,
				StartTime:      time.UnixMicro(jaegerSpan.StartTime).UTC(),
				DurationMS:     float64(jaegerSpan.Duration) / 1000,
				Tags:           tags,
				ProcessService: processService,
				Error:          errorSpan,
			})
		}
	}
	return result
}

func parentSpanID(references []jaegerReference) string {
	for _, reference := range references {
		if strings.EqualFold(reference.RefType, "CHILD_OF") {
			return reference.SpanID
		}
	}
	return ""
}

func tagMarksError(tag jaegerTag) bool {
	switch strings.ToLower(tag.Key) {
	case "error":
		value, ok := tag.Value.(bool)
		return ok && value
	case "otel.status_code", "status.code":
		value, ok := tag.Value.(string)
		return ok && strings.EqualFold(value, "ERROR")
	default:
		return false
	}
}
