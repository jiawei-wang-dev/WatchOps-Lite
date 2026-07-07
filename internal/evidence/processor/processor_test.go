package processor

import (
	"context"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
)

func TestProcessorDedupesKnowledgeChunk(t *testing.T) {
	processor := NewDefault()

	report := processor.Process(context.Background(), []evidence.Item{
		knowledgeItem("low", "chunk-1", 0.2),
		knowledgeItem("high", "chunk-1", 0.9),
	})

	if len(report.Items) != 1 {
		t.Fatalf("items = %#v, want one deduped knowledge chunk", report.Items)
	}
	if report.Items[0].ID != "high" {
		t.Fatalf("item = %#v, want higher score duplicate retained", report.Items[0])
	}
	if report.Metadata["evidence_original_count"] != 2 ||
		report.Metadata["evidence_deduped_count"] != 1 {
		t.Fatalf("metadata = %#v", report.Metadata)
	}
}

func TestProcessorScoresErrorLogAboveInfoLog(t *testing.T) {
	processor := NewDefault()

	report := processor.Process(context.Background(), []evidence.Item{
		{
			ID:      "info-log",
			Source:  evidence.SourceLogs,
			Content: "checkout request completed",
			Metadata: map[string]any{
				"log_id": "info-log",
				"level":  "info",
			},
		},
		{
			ID:      "error-log",
			Source:  evidence.SourceLogs,
			Content: "checkout upstream timeout",
			Metadata: map[string]any{
				"log_id":   "error-log",
				"level":    "error",
				"trace_id": "trace-1",
			},
		},
	})

	if len(report.Items) != 2 || report.Items[0].ID != "error-log" {
		t.Fatalf("items = %#v, want error log ranked first", report.Items)
	}
}

func TestProcessorScoresErrorSpanAboveNormalSpan(t *testing.T) {
	processor := NewDefault()

	report := processor.Process(context.Background(), []evidence.Item{
		{
			ID:      "normal-span",
			Source:  evidence.SourceTraces,
			Content: "checkout span completed",
			Metadata: map[string]any{
				"trace_id":    "trace-1",
				"span_id":     "span-1",
				"duration_ms": 120,
			},
		},
		{
			ID:      "error-span",
			Source:  evidence.SourceTraces,
			Content: "payment span failed",
			Metadata: map[string]any{
				"trace_id":    "trace-1",
				"span_id":     "span-2",
				"duration_ms": 2800,
				"error":       true,
			},
		},
	})

	if len(report.Items) != 2 || report.Items[0].ID != "error-span" {
		t.Fatalf("items = %#v, want error span ranked first", report.Items)
	}
}

func TestProcessorAssignsStableCitationsAndGroups(t *testing.T) {
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	processor := New(Config{
		Enabled:        true,
		CitationPrefix: "E",
		Now: func() time.Time {
			return now
		},
	})

	report := processor.Process(context.Background(), []evidence.Item{
		{
			ID:      "metric-1",
			Source:  evidence.SourceMetrics,
			Content: "checkout error rate is elevated",
			Metadata: map[string]any{
				"metric_name": "watchops_checkout_error_rate",
				"value":       0.08,
				"timestamp":   now.Format(time.RFC3339),
			},
		},
		knowledgeItem("knowledge-1", "chunk-1", 0.7),
	})

	if len(report.Items) != 2 ||
		report.Items[0].CitationID != "E1" ||
		report.Items[1].CitationID != "E2" ||
		report.Items[0].Metadata["citation_id"] != "E1" {
		t.Fatalf("items = %#v, want stable citation IDs", report.Items)
	}
	if len(report.Groups) != 2 ||
		report.Groups[0].Source != evidence.SourceMetrics ||
		report.Groups[1].Source != evidence.SourceKnowledge {
		t.Fatalf("groups = %#v, want metrics then knowledge", report.Groups)
	}
	if report.Metadata["citation_enabled"] != true {
		t.Fatalf("metadata = %#v, want citation enabled", report.Metadata)
	}
}

func TestProcessorHandlesEmptyEvidence(t *testing.T) {
	report := NewDefault().Process(context.Background(), nil)

	if len(report.Items) != 0 ||
		len(report.Groups) != 0 ||
		report.Metadata["evidence_original_count"] != 0 {
		t.Fatalf("report = %#v, want empty report without error", report)
	}
}

func knowledgeItem(id string, chunkID string, score float64) evidence.Item {
	return evidence.Item{
		ID:      id,
		Source:  evidence.SourceKnowledge,
		Content: "checkout runbook",
		Score:   &score,
		Metadata: map[string]any{
			"chunk_id": chunkID,
			"category": "runbook",
		},
	}
}
