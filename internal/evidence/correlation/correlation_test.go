package correlation

import (
	"context"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
)

func TestRunCorrelatesLogsMetricsAndTraces(t *testing.T) {
	items := []evidence.Item{
		{
			ID:         "log-1",
			Type:       evidence.TypeLogEvent,
			Source:     evidence.SourceLogs,
			Content:    "context deadline exceeded calling payment",
			ResourceID: "checkout",
			TraceID:    "trace-1",
			TimeRange: &evidence.TimeRange{
				From: "2026-07-01T00:10:00Z",
				To:   "2026-07-01T00:10:00Z",
			},
			Metadata: map[string]any{"service": "checkout"},
		},
		{
			ID:         "metric-1",
			Type:       evidence.TypeMetricSample,
			Source:     evidence.SourceMetrics,
			Content:    "checkout p95 latency increased",
			ResourceID: "checkout",
			TimeRange: &evidence.TimeRange{
				From: "2026-07-01T00:09:00Z",
				To:   "2026-07-01T00:11:00Z",
			},
			Metadata: map[string]any{
				"service":     "checkout",
				"metric_name": "checkout_p95_latency",
			},
		},
		{
			ID:      "trace-1-span-1",
			Type:    evidence.TypeTraceSpan,
			Source:  evidence.SourceTraces,
			Content: "payment span duration 1400ms",
			TraceID: "trace-1",
			TimeRange: &evidence.TimeRange{
				From: "2026-07-01T00:10:00Z",
				To:   "2026-07-01T00:10:02Z",
			},
			Metadata: map[string]any{
				"service":     "checkout",
				"duration_ms": 1400.0,
			},
		},
	}

	result := New().Run(context.Background(), items)

	if len(result.Evidence) != len(items) {
		t.Fatalf("evidence count = %d, want raw evidence preserved", len(result.Evidence))
	}
	if len(result.Relationships) != 3 {
		t.Fatalf("relationships = %#v, want all three cross-source pairs", result.Relationships)
	}
	if result.CorrelationScore <= 0.5 {
		t.Fatalf("correlation score = %f, want meaningful correlation", result.CorrelationScore)
	}
	assertRelationship(t, result.Relationships, RelationshipLogsMetrics)
	assertRelationship(t, result.Relationships, RelationshipLogsTraces)
	assertRelationship(t, result.Relationships, RelationshipMetricsTraces)
}

func TestRunGroupsBySignalWindowAndService(t *testing.T) {
	items := []evidence.Item{
		{
			ID:         "log-1",
			Type:       evidence.TypeLogEvent,
			Source:     evidence.SourceLogs,
			Content:    "upstream timeout",
			ResourceID: "checkout",
			TimeRange: &evidence.TimeRange{
				From: "2026-07-01T00:10:00Z",
				To:   "2026-07-01T00:10:00Z",
			},
		},
		{
			ID:         "log-2",
			Type:       evidence.TypeLogEvent,
			Source:     evidence.SourceLogs,
			Content:    "context deadline exceeded",
			ResourceID: "checkout",
			TimeRange: &evidence.TimeRange{
				From: "2026-07-01T00:12:00Z",
				To:   "2026-07-01T00:12:00Z",
			},
		},
	}

	result := New().Run(context.Background(), items)

	if len(result.Groups) != 1 ||
		len(result.Groups[0].EvidenceIDs) != 2 ||
		result.Groups[0].Signal != "timeout" ||
		result.Groups[0].Service != "checkout" {
		t.Fatalf("groups = %#v, want shared timeout incident group", result.Groups)
	}
}

func TestRunDoesNotMutateRawEvidence(t *testing.T) {
	item := evidence.Item{
		ID:      "log-1",
		Type:    evidence.TypeLogEvent,
		Source:  evidence.SourceLogs,
		Content: "original",
	}
	result := New().Run(context.Background(), []evidence.Item{item})
	result.Evidence[0].Content = "changed"
	if item.Content != "original" {
		t.Fatalf("raw evidence was mutated: %#v", item)
	}
}

func assertRelationship(
	t *testing.T,
	relationships []Relationship,
	relationshipType RelationshipType,
) {
	t.Helper()
	for _, item := range relationships {
		if item.Type == relationshipType {
			return
		}
	}
	t.Fatalf("relationships = %#v, missing %s", relationships, relationshipType)
}
