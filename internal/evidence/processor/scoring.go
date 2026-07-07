package processor

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
)

func (p *Processor) score(ctx context.Context, items []evidence.Item) []ProcessedEvidence {
	_, span := observability.StartSpan(ctx, "evidence.score")
	defer span.End()

	result := make([]ProcessedEvidence, 0, len(items))
	for _, item := range items {
		score := p.scoreItem(item)
		metadata := cloneMetadata(item.Metadata)
		result = append(result, ProcessedEvidence{
			ID:         item.ID,
			Source:     item.Source,
			SourceName: item.SourceName,
			Title:      metadataString(metadata, "title"),
			Content:    item.Content,
			ResourceID: item.ResourceID,
			TraceID:    firstNonEmpty(item.TraceID, metadataString(metadata, "trace_id")),
			Timestamp:  parseTimestamp(metadata),
			Score:      score,
			Metadata:   metadata,
			TimeRange:  item.TimeRange,
		})
	}
	return result
}

func (p *Processor) scoreItem(item evidence.Item) EvidenceScore {
	switch item.Source {
	case evidence.SourceLogs:
		return p.scoreLog(item)
	case evidence.SourceTraces:
		return scoreTrace(item)
	case evidence.SourceMetrics:
		return scoreMetric(item)
	case evidence.SourceKnowledge:
		return scoreKnowledge(item)
	default:
		return defaultScore(item, "default evidence score")
	}
}

func (p *Processor) scoreLog(item evidence.Item) EvidenceScore {
	level := strings.ToLower(metadataString(item.Metadata, "level"))
	severity := 0.35
	reason := "log evidence"
	if containsAny(level+" "+strings.ToLower(item.Content), "panic", "critical", "fatal") {
		severity = 0.95
		reason = "critical log signal"
	} else if containsAny(level+" "+strings.ToLower(item.Content), "error", "exception", "timeout") {
		severity = 0.8
		reason = "error log signal"
	} else if containsAny(level, "warn") {
		severity = 0.55
		reason = "warning log signal"
	}
	relevance := 0.65
	if firstNonEmpty(item.TraceID, metadataString(item.Metadata, "trace_id")) != "" {
		relevance += 0.15
	}
	freshness := p.freshness(parseTimestamp(item.Metadata))
	return finalizeScore(relevance, severity, freshness, confidence(item), reason)
}

func scoreTrace(item evidence.Item) EvidenceScore {
	errorSignal := boolMetadata(item.Metadata, "error") ||
		containsAny(strings.ToLower(item.Content), "error", "timeout", "failed")
	severity := 0.45
	reason := "trace span evidence"
	if errorSignal {
		severity = 0.85
		reason = "error trace span"
	}
	if duration := numericMetadata(item.Metadata, "duration_ms"); duration > 0 {
		severity = math.Max(severity, clamp(duration/5000, 0.35, 0.95))
	}
	return finalizeScore(0.7, severity, 0.6, confidence(item), reason)
}

func scoreMetric(item evidence.Item) EvidenceScore {
	name := strings.ToLower(metadataString(item.Metadata, "metric_name"))
	severity := 0.45
	reason := "metric evidence"
	if containsAny(name+" "+strings.ToLower(item.Content), "error_rate", "latency", "dependency", "saturation") {
		severity = 0.75
		reason = "reliability metric signal"
	}
	if value := numericMetadata(item.Metadata, "value"); value > 0 {
		severity = math.Max(severity, clamp(value*8, 0.35, 0.95))
	}
	return finalizeScore(0.75, severity, 0.6, confidence(item), reason)
}

func scoreKnowledge(item evidence.Item) EvidenceScore {
	relevance := 0.55
	if item.Score != nil {
		relevance = clamp(*item.Score, 0.2, 1)
	}
	for _, key := range []string{"rerank_score", "rrf_score", "bm25_score", "vector_score"} {
		if value := numericMetadata(item.Metadata, key); value > 0 {
			relevance = math.Max(relevance, clamp(value, 0.2, 1))
		}
	}
	category := strings.ToLower(metadataString(item.Metadata, "category"))
	severity := 0.35
	reason := "knowledge evidence"
	if containsAny(category, "runbook", "incident", "playbook") {
		severity = 0.6
		reason = "operational knowledge evidence"
	}
	return finalizeScore(relevance, severity, 0.5, confidence(item), reason)
}

func defaultScore(item evidence.Item, reason string) EvidenceScore {
	return finalizeScore(0.5, 0.4, 0.5, confidence(item), reason)
}

func finalizeScore(
	relevance float64,
	severity float64,
	freshness float64,
	confidence float64,
	reason string,
) EvidenceScore {
	final := relevance*0.35 + severity*0.3 + freshness*0.15 + confidence*0.2
	return EvidenceScore{
		Relevance:  roundScore(clamp(relevance, 0, 1)),
		Severity:   roundScore(clamp(severity, 0, 1)),
		Freshness:  roundScore(clamp(freshness, 0, 1)),
		Confidence: roundScore(clamp(confidence, 0, 1)),
		Final:      roundScore(clamp(final, 0, 1)),
		Reason:     reason,
	}
}

func sortProcessed(items []ProcessedEvidence) {
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].Score.Final != items[right].Score.Final {
			return items[left].Score.Final > items[right].Score.Final
		}
		leftPriority := sourcePriority(items[left].Source)
		rightPriority := sourcePriority(items[right].Source)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return items[left].ID < items[right].ID
	})
}

func (p *Processor) freshness(timestamp *time.Time) float64 {
	if timestamp == nil {
		return 0.5
	}
	hours := p.config.Now().Sub(*timestamp).Hours()
	if hours <= 1 {
		return 1
	}
	if hours >= 24*7 {
		return 0.2
	}
	return clamp(1-(hours/(24*7))*0.8, 0.2, 1)
}

func sourcePriority(source evidence.Source) int {
	switch source {
	case evidence.SourceMetrics:
		return 0
	case evidence.SourceLogs:
		return 1
	case evidence.SourceTraces:
		return 2
	case evidence.SourceKnowledge:
		return 3
	default:
		return 4
	}
}

func confidence(item evidence.Item) float64 {
	if item.Confidence != nil {
		return *item.Confidence
	}
	return 0.7
}

func clamp(value float64, low float64, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func roundScore(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func parseTimestamp(metadata map[string]any) *time.Time {
	for _, key := range []string{"timestamp", "created_at", "sample_time", "start_time"} {
		value := metadataString(metadata, key)
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func numericMetadata(metadata map[string]any, key string) float64 {
	switch value := metadata[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case jsonNumber:
		parsed, _ := value.Float64()
		return parsed
	case string:
		var parsed float64
		_, _ = fmt.Sscanf(value, "%f", &parsed)
		return parsed
	default:
		return 0
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}
