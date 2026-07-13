package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/mcp"
)

type MCPStore struct {
	client mcp.Client
}

func NewMCPStore(client mcp.Client) *MCPStore {
	return &MCPStore{client: client}
}

func (s *MCPStore) Query(
	ctx context.Context,
	expression string,
	at time.Time,
) ([]Sample, error) {
	if s.client == nil {
		return nil, ErrUnavailable
	}
	response, err := s.client.CallTool(ctx, "query_prometheus", map[string]any{
		"query": expression,
		"time":  at.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	samples, err := parseMCPSamples(response, expression)
	if err != nil {
		return nil, err
	}
	return samples, nil
}

func parseMCPSamples(response map[string]any, expression string) ([]Sample, error) {
	if samplesValue, ok := response["samples"]; ok {
		return decodeMCPList(samplesValue, expression)
	}
	if resultValue, ok := response["result"]; ok {
		return decodeMCPList(resultValue, expression)
	}
	if data, ok := response["data"].(map[string]any); ok {
		if resultValue, ok := data["result"]; ok {
			return decodeMCPList(resultValue, expression)
		}
	}
	return nil, fmt.Errorf("%w: MCP response did not include metric samples", ErrUnavailable)
}

func decodeMCPList(value any, expression string) ([]Sample, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: encode MCP samples: %v", ErrUnavailable, err)
	}
	var items []map[string]any
	if err := json.Unmarshal(encoded, &items); err != nil {
		return nil, fmt.Errorf("%w: decode MCP samples: %v", ErrUnavailable, err)
	}
	samples := make([]Sample, 0, len(items))
	for _, item := range items {
		sample, err := decodeMCPSample(item, expression)
		if err != nil {
			return nil, err
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

func decodeMCPSample(item map[string]any, expression string) (Sample, error) {
	if metric, ok := item["metric"].(map[string]any); ok {
		return decodePrometheusStyleSample(metric, item["value"], expression)
	}
	name := stringValue(item["name"])
	if name == "" {
		name = stringValue(item["metric_name"])
	}
	timestamp, err := parseMCPTimestamp(item["timestamp"])
	if err != nil {
		return Sample{}, err
	}
	value, err := parseMCPFloat(item["value"])
	if err != nil {
		return Sample{}, err
	}
	labels := mapStringValues(item["labels"])
	service := stringValue(item["service"])
	if service == "" {
		service = labels["service"]
	}
	return Sample{
		Name:      name,
		Value:     value,
		Timestamp: timestamp,
		Service:   service,
		Labels:    labels,
		Query:     expression,
	}, nil
}

func decodePrometheusStyleSample(metric map[string]any, value any, expression string) (Sample, error) {
	timestamp, sampleValue, err := parsePrometheusStyleValue(value)
	if err != nil {
		return Sample{}, err
	}
	labels := make(map[string]string, len(metric))
	for name, labelValue := range metric {
		if name == "__name__" {
			continue
		}
		labels[name] = stringValue(labelValue)
	}
	return Sample{
		Name:      stringValue(metric["__name__"]),
		Value:     sampleValue,
		Timestamp: timestamp,
		Service:   stringValue(metric["service"]),
		Labels:    labels,
		Query:     expression,
	}, nil
}

func parsePrometheusStyleValue(value any) (time.Time, float64, error) {
	items, ok := value.([]any)
	if !ok || len(items) != 2 {
		return time.Time{}, 0, fmt.Errorf("%w: Prometheus-style value must contain timestamp and value", ErrUnavailable)
	}
	timestamp, err := parseMCPTimestamp(items[0])
	if err != nil {
		return time.Time{}, 0, err
	}
	sampleValue, err := parseMCPFloat(items[1])
	if err != nil {
		return time.Time{}, 0, err
	}
	return timestamp, sampleValue, nil
}

func parseMCPTimestamp(value any) (time.Time, error) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
			return parsed.UTC(), nil
		}
		seconds, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: invalid sample timestamp", ErrUnavailable)
		}
		return unixFloat(seconds), nil
	case float64:
		return unixFloat(typed), nil
	default:
		return time.Time{}, fmt.Errorf("%w: invalid sample timestamp", ErrUnavailable)
	}
}

func parseMCPFloat(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, fmt.Errorf("%w: invalid sample value", ErrUnavailable)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%w: invalid sample value", ErrUnavailable)
	}
}

func unixFloat(seconds float64) time.Time {
	whole := int64(seconds)
	fraction := seconds - float64(whole)
	return time.Unix(whole, int64(fraction*float64(time.Second))).UTC()
}

func mapStringValues(value any) map[string]string {
	values := map[string]string{}
	raw, ok := value.(map[string]any)
	if !ok {
		return values
	}
	for key, item := range raw {
		values[key] = stringValue(item)
	}
	return values
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}
