package summary

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
)

func TestDeterministicSummaryPreservesOperationalContext(t *testing.T) {
	result, err := NewDeterministic().Summarize(
		context.Background(),
		session.EmptySummary(),
		[]session.Message{
			{
				Role:      session.RoleUser,
				Content:   "Why did checkout error rate increase?",
				CreatedAt: time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
				RequestID: "req-01",
				Metadata: map[string]any{
					"time_range": map[string]any{
						"from": "2026-06-30T00:00:00Z",
						"to":   "2026-06-30T00:20:00Z",
					},
				},
			},
			{
				Role:      session.RoleAssistant,
				Content:   "Mock logs report upstream timeout errors.",
				CreatedAt: time.Date(2026, 6, 30, 0, 1, 0, 0, time.UTC),
				RequestID: "req-01",
				Metadata: map[string]any{
					"tool_names":   []string{"query_logs"},
					"error_codes":  []string{"TOOL_TIMEOUT"},
					"resource_ids": []string{"checkout"},
					"trace_ids":    []string{"trace-01"},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}

	if result.Goal != "Why did checkout error rate increase?" {
		t.Fatalf("goal = %q, want first user goal", result.Goal)
	}
	if !contains(result.AttemptedActions, "query_logs") {
		t.Fatalf("attempted actions = %#v, want query_logs", result.AttemptedActions)
	}
	if !contains(result.ImportantEntities, "checkout") ||
		!contains(result.ImportantEntities, "trace-01") ||
		!contains(result.ImportantEntities, "request_id:req-01") {
		t.Fatalf("important entities = %#v, missing preserved identifiers", result.ImportantEntities)
	}
	if !contains(result.ConfirmedFacts, "tool_error:TOOL_TIMEOUT") {
		t.Fatalf("confirmed facts = %#v, want error code", result.ConfirmedFacts)
	}
	if !strings.Contains(result.Content, "checkout") {
		t.Fatalf("content = %q, want service name preserved", result.Content)
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
