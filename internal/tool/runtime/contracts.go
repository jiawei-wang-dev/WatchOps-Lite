package runtime

import (
	"context"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
)

type SourceType = evidence.Source

const (
	SourceLogs      = evidence.SourceLogs
	SourceMetrics   = evidence.SourceMetrics
	SourceTraces    = evidence.SourceTraces
	SourceKnowledge = evidence.SourceKnowledge
)

const (
	ErrorCodeInvalidArgument       = "TOOL_INVALID_ARGUMENT"
	ErrorCodeTimeout               = "TOOL_TIMEOUT"
	ErrorCodeDependencyUnavailable = "TOOL_DEPENDENCY_UNAVAILABLE"
	ErrorCodeRateLimited           = "TOOL_RATE_LIMITED"
	ErrorCodeInternal              = "TOOL_INTERNAL"
)

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	Tool       string          `json:"tool"`
	SourceType SourceType      `json:"source_type"`
	Evidence   []evidence.Item `json:"evidence"`
	Payload    map[string]any  `json:"payload,omitempty"`
	Warnings   []Warning       `json:"warnings"`
	Metadata   map[string]any  `json:"metadata"`
	Error      *ToolError      `json:"error,omitempty"`
	LatencyMS  int64           `json:"latency_ms"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
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
