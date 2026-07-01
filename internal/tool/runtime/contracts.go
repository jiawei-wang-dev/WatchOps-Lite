package runtime

import (
	"context"
	"time"
)

type SourceType string

const (
	SourceLogs      SourceType = "logs"
	SourceMetrics   SourceType = "metrics"
	SourceTraces    SourceType = "traces"
	SourceKnowledge SourceType = "knowledge"
)

const (
	ErrorCodeInvalidArgument       = "TOOL_INVALID_ARGUMENT"
	ErrorCodeTimeout               = "TOOL_TIMEOUT"
	ErrorCodeDependencyUnavailable = "TOOL_DEPENDENCY_UNAVAILABLE"
	ErrorCodeRateLimited           = "TOOL_RATE_LIMITED"
	ErrorCodeInternal              = "TOOL_INTERNAL"
)

type TimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Evidence struct {
	EvidenceID string         `json:"evidence_id"`
	SourceType SourceType     `json:"source_type"`
	Source     string         `json:"source,omitempty"`
	Content    string         `json:"content"`
	Score      *float64       `json:"score,omitempty"`
	TimeRange  *TimeRange     `json:"time_range,omitempty"`
	TraceID    string         `json:"trace_id,omitempty"`
	ResourceID string         `json:"resource_id,omitempty"`
	Confidence *float64       `json:"confidence,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	Tool       string         `json:"tool"`
	SourceType SourceType     `json:"source_type"`
	Evidence   []Evidence     `json:"evidence"`
	Payload    map[string]any `json:"payload,omitempty"`
	Warnings   []Warning      `json:"warnings"`
	Metadata   map[string]any `json:"metadata"`
	Error      *ToolError     `json:"error,omitempty"`
	LatencyMS  int64          `json:"latency_ms"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
}

type ToolError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Source    string         `json:"source"`
	Details   map[string]any `json:"details,omitempty"`
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func NewToolError(
	code string,
	source string,
	message string,
	retryable bool,
	details map[string]any,
) *ToolError {
	return &ToolError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		Source:    source,
		Details:   details,
	}
}

type Tool interface {
	Name() string
	SourceType() SourceType
	Execute(ctx context.Context, input any) Result
}
