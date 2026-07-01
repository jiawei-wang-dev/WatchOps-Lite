package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	retrievalmetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type searcherStub struct {
	samples []retrievalmetrics.Sample
	err     error
	request retrievalmetrics.QueryRequest
}

func (s *searcherStub) Query(
	_ context.Context,
	request retrievalmetrics.QueryRequest,
) ([]retrievalmetrics.Sample, error) {
	s.request = request
	return s.samples, s.err
}

func TestSearchToolMapsPrometheusSamplesToEvidence(t *testing.T) {
	timestamp := time.Date(2026, 6, 30, 0, 20, 0, 0, time.UTC)
	searcher := &searcherStub{samples: []retrievalmetrics.Sample{{
		Name:      "watchops_checkout_error_rate",
		Value:     0.062,
		Timestamp: timestamp,
		Service:   "checkout",
		Labels:    map[string]string{"environment": "demo"},
		Query:     "watchops_checkout_error_rate",
	}}}
	tool := NewSearchTool(searcher, SearchToolConfig{
		Backend:        "prometheus",
		BaseURL:        "http://localhost:9090",
		FallbackToMock: true,
	})

	result, err := tool.Execute(context.Background(), validMetricsInput())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Evidence) != 1 ||
		result.Evidence[0].SourceName != "prometheus" ||
		result.Evidence[0].ResourceID != "checkout" ||
		result.Metadata["fallback_used"] != false {
		t.Fatalf("result = %#v", result)
	}
	metadata := result.Evidence[0].Metadata
	if metadata["metric_name"] != "watchops_checkout_error_rate" ||
		metadata["value"] != 0.062 ||
		metadata["query"] != "watchops_checkout_error_rate" {
		t.Fatalf("evidence metadata = %#v", metadata)
	}
	if searcher.request.Service != "checkout" ||
		searcher.request.MetricName != "http_server_error_rate" ||
		!searcher.request.At.Equal(timestamp) {
		t.Fatalf("query request = %#v", searcher.request)
	}
}

func TestSearchToolFallsBackToMockWhenPrometheusIsUnavailable(t *testing.T) {
	searcher := &searcherStub{err: retrievalmetrics.ErrUnavailable}
	tool := NewSearchTool(searcher, SearchToolConfig{
		Backend:        "prometheus",
		BaseURL:        "http://localhost:9090",
		FallbackToMock: true,
	})

	result, err := tool.Execute(context.Background(), validMetricsInput())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Metadata["mode"] != "mock_fallback" ||
		result.Metadata["fallback_used"] != true ||
		len(result.Warnings) != 1 ||
		result.Warnings[0].Code != "METRICS_FALLBACK" ||
		result.Evidence[0].SourceName != "mock-metrics" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchToolReportsEmptyPrometheusResultWithoutMockingData(t *testing.T) {
	tool := NewSearchTool(&searcherStub{}, SearchToolConfig{
		Backend:        "prometheus",
		BaseURL:        "http://localhost:9090",
		FallbackToMock: true,
	})

	result, err := tool.Execute(context.Background(), validMetricsInput())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Evidence) != 0 ||
		len(result.Warnings) != 1 ||
		result.Warnings[0].Code != "METRICS_NO_DATA" ||
		result.Metadata["mode"] != "prometheus" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchToolReturnsStructuredErrorWhenFallbackIsDisabled(t *testing.T) {
	tool := NewSearchTool(&searcherStub{err: errors.New("connection refused")}, SearchToolConfig{
		Backend:        "prometheus",
		BaseURL:        "http://localhost:9090",
		FallbackToMock: false,
	})

	result, err := tool.Execute(context.Background(), validMetricsInput())
	var toolErr *common.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("error = %v, want *common.ToolError", err)
	}
	if toolErr.Code != common.ErrorCodeDependencyUnavailable ||
		result.Error == nil ||
		result.Error.Code != common.ErrorCodeDependencyUnavailable {
		t.Fatalf("result=%#v error=%#v", result, toolErr)
	}
}

func validMetricsInput() Input {
	return Input{
		Service:    "checkout",
		MetricName: "http_server_error_rate",
		TimeRange: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
	}
}
