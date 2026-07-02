package evidence

import (
	"errors"
	"strings"
)

type Source string

const (
	SourceLogs      Source = "logs"
	SourceMetrics   Source = "metrics"
	SourceTraces    Source = "traces"
	SourceKnowledge Source = "knowledge"
	SourceAlerts    Source = "alerts"
	SourceTopology  Source = "topology"
)

const (
	TypeLogEvent       = "log_event"
	TypeMetricSample   = "metric_sample"
	TypeTraceSpan      = "trace_span"
	TypeKnowledgeChunk = "knowledge_chunk"
	TypeAlertSignal    = "alert_signal"
	TypeTopology       = "service_topology"
)

type TimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Item struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Source     Source         `json:"source"`
	SourceName string         `json:"source_name,omitempty"`
	Content    string         `json:"content"`
	Score      *float64       `json:"score,omitempty"`
	TimeRange  *TimeRange     `json:"time_range,omitempty"`
	TraceID    string         `json:"trace_id,omitempty"`
	ResourceID string         `json:"resource_id,omitempty"`
	Confidence *float64       `json:"confidence,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func Normalize(item Item, defaultSource Source) Item {
	item.ID = strings.TrimSpace(item.ID)
	item.Type = strings.TrimSpace(item.Type)
	item.SourceName = strings.TrimSpace(item.SourceName)
	item.Content = strings.TrimSpace(item.Content)
	item.TraceID = strings.TrimSpace(item.TraceID)
	item.ResourceID = strings.TrimSpace(item.ResourceID)
	if item.Source == "" {
		item.Source = defaultSource
	}
	if item.Type == "" {
		item.Type = defaultType(item.Source)
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	if item.TraceID == "" {
		if traceID, ok := item.Metadata["trace_id"].(string); ok {
			item.TraceID = strings.TrimSpace(traceID)
		}
	}
	return item
}

func Validate(item Item) error {
	if strings.TrimSpace(item.ID) == "" {
		return errors.New("evidence id is required")
	}
	switch item.Source {
	case SourceLogs, SourceMetrics, SourceTraces, SourceKnowledge, SourceAlerts, SourceTopology:
	default:
		return errors.New("evidence source is invalid")
	}
	if strings.TrimSpace(item.Type) == "" {
		return errors.New("evidence type is required")
	}
	if strings.TrimSpace(item.Content) == "" {
		return errors.New("evidence content is required")
	}
	return nil
}

func defaultType(source Source) string {
	switch source {
	case SourceLogs:
		return TypeLogEvent
	case SourceMetrics:
		return TypeMetricSample
	case SourceTraces:
		return TypeTraceSpan
	case SourceKnowledge:
		return TypeKnowledgeChunk
	case SourceAlerts:
		return TypeAlertSignal
	case SourceTopology:
		return TypeTopology
	default:
		return "evidence"
	}
}
