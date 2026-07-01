package logs

import (
	"context"
	"errors"
	"testing"
	"time"

	retrievallogs "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type searcherStub struct {
	events []retrievallogs.Event
	err    error
	query  retrievallogs.SearchQuery
}

func (s *searcherStub) Search(
	_ context.Context,
	query retrievallogs.SearchQuery,
) ([]retrievallogs.Event, error) {
	s.query = query
	return s.events, s.err
}

func TestSearchToolMapsElasticsearchLogsToEvidence(t *testing.T) {
	timestamp := time.Date(2026, 6, 30, 0, 10, 0, 0, time.UTC)
	searcher := &searcherStub{events: []retrievallogs.Event{{
		ID:        "log_checkout_001",
		Timestamp: timestamp,
		Service:   "checkout",
		Level:     "error",
		Message:   "upstream timeout calling payment",
		TraceID:   "trace-001",
		SpanID:    "span-001",
	}}}
	tool := NewSearchTool(searcher, SearchToolConfig{
		Backend:        "elasticsearch",
		Index:          "watchops_logs",
		DefaultLimit:   20,
		FallbackToMock: true,
	})

	result, err := tool.Execute(context.Background(), validLogsInput())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Evidence) != 1 ||
		result.Evidence[0].SourceName != "elasticsearch-logs" ||
		result.Evidence[0].Content != "upstream timeout calling payment" ||
		result.Metadata["fallback_used"] != false {
		t.Fatalf("result = %#v", result)
	}
	metadata := result.Evidence[0].Metadata
	if metadata["log_id"] != "log_checkout_001" ||
		metadata["trace_id"] != "trace-001" ||
		metadata["span_id"] != "span-001" ||
		metadata["level"] != "error" {
		t.Fatalf("evidence metadata = %#v", metadata)
	}
	if searcher.query.Limit != 20 || searcher.query.Service != "checkout" {
		t.Fatalf("search query = %#v", searcher.query)
	}
}

func TestSearchToolFallsBackToMockWhenElasticsearchIsUnavailable(t *testing.T) {
	searcher := &searcherStub{err: retrievallogs.ErrUnavailable}
	tool := NewSearchTool(searcher, SearchToolConfig{
		Backend:        "elasticsearch",
		Index:          "watchops_logs",
		DefaultLimit:   20,
		FallbackToMock: true,
	})

	result, err := tool.Execute(context.Background(), validLogsInput())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Metadata["mode"] != "mock_fallback" ||
		result.Metadata["fallback_used"] != true ||
		len(result.Warnings) != 1 ||
		result.Warnings[0].Code != "LOGS_FALLBACK" ||
		result.Evidence[0].SourceName != "mock-logs" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchToolReturnsStructuredErrorWhenFallbackIsDisabled(t *testing.T) {
	tool := NewSearchTool(&searcherStub{err: errors.New("connection refused")}, SearchToolConfig{
		Backend:        "elasticsearch",
		Index:          "watchops_logs",
		DefaultLimit:   20,
		FallbackToMock: false,
	})

	result, err := tool.Execute(context.Background(), validLogsInput())
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

func TestSearchToolBoundsLogMessageEvidence(t *testing.T) {
	searcher := &searcherStub{events: []retrievallogs.Event{{
		ID:        "log_large",
		Timestamp: time.Date(2026, 6, 30, 0, 10, 0, 0, time.UTC),
		Service:   "checkout",
		Level:     "error",
		Message:   repeatRune('x', maxEvidenceMessageRunes+1),
	}}}
	tool := NewSearchTool(searcher, SearchToolConfig{
		Backend:        "elasticsearch",
		Index:          "watchops_logs",
		DefaultLimit:   20,
		FallbackToMock: true,
	})

	result, err := tool.Execute(context.Background(), validLogsInput())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Evidence[0].Metadata["truncated"] != true ||
		len([]rune(result.Evidence[0].Content)) != maxEvidenceMessageRunes+1 {
		t.Fatalf("evidence = %#v", result.Evidence[0])
	}
}

func repeatRune(value rune, count int) string {
	runes := make([]rune, count)
	for index := range runes {
		runes[index] = value
	}
	return string(runes)
}

func validLogsInput() Input {
	return Input{
		Service: "checkout",
		TimeRange: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
		Keywords: []string{"error", "timeout"},
		Level:    "error",
	}
}
