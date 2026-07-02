package correlation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type RelationshipType string

const (
	RelationshipLogsMetrics   RelationshipType = "logs_metrics"
	RelationshipLogsTraces    RelationshipType = "logs_traces"
	RelationshipMetricsTraces RelationshipType = "metrics_traces"
)

type Relationship struct {
	LeftEvidenceID  string
	RightEvidenceID string
	Type            RelationshipType
	Score           float64
	Signals         []string
}

type Group struct {
	Key         string
	Signal      string
	Service     string
	WindowStart string
	EvidenceIDs []string
}

type CorrelatedEvidenceSet struct {
	Evidence         []evidence.Item
	Relationships    []Relationship
	Groups           []Group
	CorrelationScore float64
}

type Engine struct {
	windowSize time.Duration
}

func New() *Engine {
	return &Engine{windowSize: 5 * time.Minute}
}

func (e *Engine) Run(
	ctx context.Context,
	items []evidence.Item,
) CorrelatedEvidenceSet {
	ctx, span := observability.StartSpan(
		ctx,
		"evidence.correlation.run",
		attribute.Int("evidence_count", len(items)),
	)
	defer span.End()

	result := CorrelatedEvidenceSet{
		Evidence:      append([]evidence.Item(nil), items...),
		Relationships: []Relationship{},
		Groups:        groupEvidence(items, e.windowSize),
	}
	for left := 0; left < len(items); left++ {
		for right := left + 1; right < len(items); right++ {
			relationship, ok := correlate(items[left], items[right])
			if ok {
				result.Relationships = append(result.Relationships, relationship)
			}
		}
	}
	sort.SliceStable(result.Relationships, func(left, right int) bool {
		if result.Relationships[left].Score != result.Relationships[right].Score {
			return result.Relationships[left].Score > result.Relationships[right].Score
		}
		if result.Relationships[left].LeftEvidenceID != result.Relationships[right].LeftEvidenceID {
			return result.Relationships[left].LeftEvidenceID <
				result.Relationships[right].LeftEvidenceID
		}
		return result.Relationships[left].RightEvidenceID <
			result.Relationships[right].RightEvidenceID
	})
	result.CorrelationScore = averageScore(result.Relationships)
	span.SetAttributes(
		attribute.Float64("correlation_score", result.CorrelationScore),
		attribute.Int("relationship_count", len(result.Relationships)),
		attribute.Int("group_count", len(result.Groups)),
		attribute.StringSlice("relationship_types", relationshipTypes(result.Relationships)),
	)
	return result
}

func correlate(left, right evidence.Item) (Relationship, bool) {
	if left.Source == right.Source {
		return Relationship{}, false
	}
	if left.Source > right.Source {
		left, right = right, left
	}
	switch {
	case sourcePair(left, right, evidence.SourceLogs, evidence.SourceMetrics):
		return correlateLogsMetrics(left, right)
	case sourcePair(left, right, evidence.SourceLogs, evidence.SourceTraces):
		return correlateLogsTraces(left, right)
	case sourcePair(left, right, evidence.SourceMetrics, evidence.SourceTraces):
		return correlateMetricsTraces(left, right)
	default:
		return Relationship{}, false
	}
}

func correlateLogsMetrics(left, right evidence.Item) (Relationship, bool) {
	logItem, metricItem := bySource(left, right, evidence.SourceLogs)
	score := 0.0
	signals := []string{}
	if sameNonEmpty(serviceOf(logItem), serviceOf(metricItem)) {
		score += 0.35
		signals = append(signals, "service_match")
	}
	if timeAligned(logItem, metricItem) {
		score += 0.45
		signals = append(signals, "time_aligned")
	}
	if signalCompatible(signalOf(logItem), signalOf(metricItem)) {
		score += 0.20
		signals = append(signals, "incident_signal_match")
	}
	return relationship(logItem, metricItem, RelationshipLogsMetrics, score, signals)
}

func correlateLogsTraces(left, right evidence.Item) (Relationship, bool) {
	logItem, traceItem := bySource(left, right, evidence.SourceLogs)
	score := 0.0
	signals := []string{}
	if sameNonEmpty(traceIDOf(logItem), traceIDOf(traceItem)) {
		score += 0.65
		signals = append(signals, "trace_id_match")
	}
	if sameNonEmpty(requestIDOf(logItem), requestIDOf(traceItem)) {
		score += 0.65
		signals = append(signals, "request_id_match")
	}
	if sameNonEmpty(serviceOf(logItem), serviceOf(traceItem)) {
		score += 0.15
		signals = append(signals, "service_match")
	}
	if timeAligned(logItem, traceItem) {
		score += 0.20
		signals = append(signals, "time_aligned")
	}
	return relationship(logItem, traceItem, RelationshipLogsTraces, min(score, 1), signals)
}

func correlateMetricsTraces(left, right evidence.Item) (Relationship, bool) {
	metricItem, traceItem := bySource(left, right, evidence.SourceMetrics)
	score := 0.0
	signals := []string{}
	if sameNonEmpty(serviceOf(metricItem), serviceOf(traceItem)) {
		score += 0.30
		signals = append(signals, "service_match")
	}
	if timeAligned(metricItem, traceItem) {
		score += 0.35
		signals = append(signals, "time_aligned")
	}
	if latencySignal(metricItem) && latencySignal(traceItem) {
		score += 0.35
		signals = append(signals, "latency_signal_match")
	}
	return relationship(metricItem, traceItem, RelationshipMetricsTraces, score, signals)
}

func relationship(
	left evidence.Item,
	right evidence.Item,
	relationshipType RelationshipType,
	score float64,
	signals []string,
) (Relationship, bool) {
	if score < 0.4 {
		return Relationship{}, false
	}
	return Relationship{
		LeftEvidenceID:  left.ID,
		RightEvidenceID: right.ID,
		Type:            relationshipType,
		Score:           score,
		Signals:         signals,
	}, true
}

func groupEvidence(items []evidence.Item, windowSize time.Duration) []Group {
	groups := map[string]*Group{}
	order := []string{}
	for _, item := range items {
		service := serviceOf(item)
		if service == "" {
			service = "unknown"
		}
		signal := signalOf(item)
		windowStart := windowOf(item, windowSize)
		key := service + "|" + signal + "|" + windowStart
		group, ok := groups[key]
		if !ok {
			group = &Group{
				Key:         key,
				Signal:      signal,
				Service:     service,
				WindowStart: windowStart,
				EvidenceIDs: []string{},
			}
			groups[key] = group
			order = append(order, key)
		}
		group.EvidenceIDs = append(group.EvidenceIDs, item.ID)
	}
	result := make([]Group, 0, len(order))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	return result
}

func serviceOf(item evidence.Item) string {
	for _, key := range []string{"service", "service_name"} {
		if value, ok := stringMetadata(item.Metadata, key); ok {
			return value
		}
	}
	if nested, ok := item.Metadata["metadata"].(map[string]any); ok {
		if value, ok := stringMetadata(nested, "service"); ok {
			return value
		}
	}
	if item.Source == evidence.SourceLogs || item.Source == evidence.SourceMetrics {
		return strings.TrimSpace(item.ResourceID)
	}
	return ""
}

func traceIDOf(item evidence.Item) string {
	if value := strings.TrimSpace(item.TraceID); value != "" {
		return value
	}
	value, _ := stringMetadata(item.Metadata, "trace_id")
	return value
}

func requestIDOf(item evidence.Item) string {
	value, _ := stringMetadata(item.Metadata, "request_id")
	return value
}

func signalOf(item evidence.Item) string {
	content := strings.ToLower(item.Content)
	if containsAny(content, "timeout", "deadline exceeded") {
		return "timeout"
	}
	if latencySignal(item) {
		return "latency"
	}
	if containsAny(content, "error", "failed", "exception") {
		return "error"
	}
	if value, ok := stringMetadata(item.Metadata, "metric_name"); ok {
		return strings.ToLower(value)
	}
	if value, ok := stringMetadata(item.Metadata, "level"); ok {
		return strings.ToLower(value)
	}
	return string(item.Source)
}

func latencySignal(item evidence.Item) bool {
	content := strings.ToLower(item.Content)
	if containsAny(content, "latency", "slow", "duration", "p95", "p99") {
		return true
	}
	for _, key := range []string{"metric_name", "operation"} {
		if value, ok := stringMetadata(item.Metadata, key); ok &&
			containsAny(strings.ToLower(value), "latency", "duration", "slow") {
			return true
		}
	}
	if value, ok := numericMetadata(item.Metadata, "duration_ms"); ok {
		return value > 0
	}
	return false
}

func timeAligned(left, right evidence.Item) bool {
	leftStart, leftEnd, leftOK := evidenceTimes(left)
	rightStart, rightEnd, rightOK := evidenceTimes(right)
	if !leftOK || !rightOK {
		return false
	}
	const tolerance = 2 * time.Minute
	return !leftEnd.Add(tolerance).Before(rightStart) &&
		!rightEnd.Add(tolerance).Before(leftStart)
}

func evidenceTimes(item evidence.Item) (time.Time, time.Time, bool) {
	if item.TimeRange != nil {
		from, fromErr := time.Parse(time.RFC3339Nano, item.TimeRange.From)
		to, toErr := time.Parse(time.RFC3339Nano, item.TimeRange.To)
		if fromErr == nil && toErr == nil {
			return from, to, true
		}
	}
	for _, key := range []string{"timestamp", "start_time"} {
		if value, ok := stringMetadata(item.Metadata, key); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				return parsed, parsed, true
			}
		}
	}
	return time.Time{}, time.Time{}, false
}

func windowOf(item evidence.Item, size time.Duration) string {
	start, _, ok := evidenceTimes(item)
	if !ok {
		return "unknown"
	}
	return start.UTC().Truncate(size).Format(time.RFC3339)
}

func sourcePair(
	left evidence.Item,
	right evidence.Item,
	first evidence.Source,
	second evidence.Source,
) bool {
	return (left.Source == first && right.Source == second) ||
		(left.Source == second && right.Source == first)
}

func bySource(
	left evidence.Item,
	right evidence.Item,
	source evidence.Source,
) (evidence.Item, evidence.Item) {
	if left.Source == source {
		return left, right
	}
	return right, left
}

func stringMetadata(metadata map[string]any, key string) (string, bool) {
	value, ok := metadata[key].(string)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

func numericMetadata(metadata map[string]any, key string) (float64, bool) {
	switch value := metadata[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

func averageScore(relationships []Relationship) float64 {
	if len(relationships) == 0 {
		return 0
	}
	total := 0.0
	for _, item := range relationships {
		total += item.Score
	}
	return total / float64(len(relationships))
}

func relationshipTypes(relationships []Relationship) []string {
	seen := map[RelationshipType]struct{}{}
	result := []string{}
	for _, item := range relationships {
		if _, ok := seen[item.Type]; ok {
			continue
		}
		seen[item.Type] = struct{}{}
		result = append(result, string(item.Type))
	}
	return result
}

func sameNonEmpty(left, right string) bool {
	return left != "" && left == right
}

func signalCompatible(left, right string) bool {
	if left == right {
		return true
	}
	return (left == "timeout" && right == "latency") ||
		(left == "latency" && right == "timeout") ||
		(left == "error" && right == "timeout") ||
		(left == "timeout" && right == "error")
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func min(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func (r Relationship) String() string {
	return fmt.Sprintf(
		"%s:%s:%s:%.2f",
		r.Type,
		r.LeftEvidenceID,
		r.RightEvidenceID,
		r.Score,
	)
}
