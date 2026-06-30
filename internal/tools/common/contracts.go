package common

import (
	"fmt"
	"time"
)

type ToolErrorCode string

const (
	ErrorCodeInvalidArgument       ToolErrorCode = "TOOL_INVALID_ARGUMENT"
	ErrorCodeTimeout               ToolErrorCode = "TOOL_TIMEOUT"
	ErrorCodeDependencyUnavailable ToolErrorCode = "TOOL_DEPENDENCY_UNAVAILABLE"
	ErrorCodeRateLimited           ToolErrorCode = "TOOL_RATE_LIMITED"
	ErrorCodeInternal              ToolErrorCode = "TOOL_INTERNAL"
)

type TimeRange struct {
	From string `json:"from" jsonschema:"required,description=RFC3339 start time"`
	To   string `json:"to" jsonschema:"required,description=RFC3339 end time"`
}

func (r TimeRange) Validate() error {
	from, err := time.Parse(time.RFC3339, r.From)
	if err != nil {
		return fmt.Errorf("from must be an RFC3339 timestamp")
	}

	to, err := time.Parse(time.RFC3339, r.To)
	if err != nil {
		return fmt.Errorf("to must be an RFC3339 timestamp")
	}
	if to.Before(from) {
		return fmt.Errorf("to must not be before from")
	}

	return nil
}

type EvidenceItem struct {
	ID         string         `json:"id"`
	SourceType string         `json:"source_type"`
	SourceName string         `json:"source_name"`
	TimeRange  *TimeRange     `json:"time_range,omitempty"`
	Content    string         `json:"content"`
	ResourceID string         `json:"resource_id,omitempty"`
	Score      *float64       `json:"score,omitempty"`
	Confidence *float64       `json:"confidence,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ToolWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ToolResult struct {
	Tool       string         `json:"tool"`
	Success    bool           `json:"success"`
	Evidence   []EvidenceItem `json:"evidence"`
	Payload    map[string]any `json:"payload,omitempty"`
	Warnings   []ToolWarning  `json:"warnings"`
	Metadata   map[string]any `json:"metadata"`
	Error      *ToolError     `json:"error,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	DurationMS int64          `json:"duration_ms"`
}

type ToolError struct {
	Code      ToolErrorCode  `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Tool      string         `json:"tool"`
	Details   map[string]any `json:"details,omitempty"`
	Fallback  string         `json:"fallback,omitempty"`
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

func NewToolError(
	code ToolErrorCode,
	tool string,
	message string,
	retryable bool,
	details map[string]any,
	fallback string,
) *ToolError {
	return &ToolError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		Tool:      tool,
		Details:   details,
		Fallback:  fallback,
	}
}

func InvalidArgument(tool string, message string, details map[string]any) *ToolError {
	return NewToolError(
		ErrorCodeInvalidArgument,
		tool,
		message,
		false,
		details,
		"correct the tool arguments and retry",
	)
}
