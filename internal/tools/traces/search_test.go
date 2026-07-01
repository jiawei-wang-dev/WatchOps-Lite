package traces

import (
	"context"
	"errors"
	"testing"
	"time"

	retrievaltraces "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/traces"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type searcherStub struct {
	spans []retrievaltraces.Span
	err   error
	query retrievaltraces.Query
}

func (s *searcherStub) Search(
	_ context.Context,
	query retrievaltraces.Query,
) ([]retrievaltraces.Span, error) {
	s.query = query
	return s.spans, s.err
}

func TestSearchToolMapsJaegerSpansToEvidence(t *testing.T) {
	start := time.Date(2026, 7, 1, 5, 10, 0, 0, time.UTC)
	searcher := &searcherStub{spans: []retrievaltraces.Span{{
		TraceID:    "trace-001",
		SpanID:     "span-001",
		Service:    "watchops-lite",
		Operation:  "agent.run",
		StartTime:  start,
		DurationMS: 125.5,
		Error:      true,
	}}}
	tool := NewSearchTool(searcher, SearchToolConfig{
		Backend:        "jaeger",
		BaseURL:        "http://localhost:16686",
		DefaultService: "watchops-lite",
		DefaultLimit:   10,
		FallbackToMock: true,
	})

	result, err := tool.Execute(context.Background(), validTracesInput())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Evidence) != 1 ||
		result.Evidence[0].SourceName != "jaeger" ||
		result.Evidence[0].ResourceID != "trace-001" ||
		result.Metadata["fallback_used"] != false {
		t.Fatalf("result = %#v", result)
	}
	metadata := result.Evidence[0].Metadata
	if metadata["trace_id"] != "trace-001" ||
		metadata["span_id"] != "span-001" ||
		metadata["operation"] != "agent.run" ||
		metadata["duration_ms"] != 125.5 ||
		metadata["error"] != true {
		t.Fatalf("metadata = %#v", metadata)
	}
	if searcher.query.Limit != 10 || searcher.query.Service != "watchops-lite" {
		t.Fatalf("query = %#v", searcher.query)
	}
}

func TestSearchToolFallsBackToMockWhenJaegerIsUnavailable(t *testing.T) {
	tool := NewSearchTool(
		&searcherStub{err: retrievaltraces.ErrUnavailable},
		SearchToolConfig{
			Backend:        "jaeger",
			BaseURL:        "http://localhost:16686",
			DefaultService: "watchops-lite",
			DefaultLimit:   10,
			FallbackToMock: true,
		},
	)

	result, err := tool.Execute(context.Background(), validTracesInput())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Metadata["mode"] != "mock_fallback" ||
		result.Metadata["fallback_used"] != true ||
		len(result.Warnings) != 1 ||
		result.Warnings[0].Code != "TRACES_FALLBACK" ||
		result.Evidence[0].SourceName != "mock-traces" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchToolReturnsStructuredErrorWhenFallbackIsDisabled(t *testing.T) {
	tool := NewSearchTool(
		&searcherStub{err: errors.New("connection refused")},
		SearchToolConfig{
			Backend:        "jaeger",
			BaseURL:        "http://localhost:16686",
			DefaultService: "watchops-lite",
			DefaultLimit:   10,
			FallbackToMock: false,
		},
	)

	result, err := tool.Execute(context.Background(), validTracesInput())
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

func validTracesInput() Input {
	return Input{
		Service: "watchops-lite",
		TimeRange: common.TimeRange{
			From: "2026-07-01T05:00:00Z",
			To:   "2026-07-01T05:20:00Z",
		},
	}
}
