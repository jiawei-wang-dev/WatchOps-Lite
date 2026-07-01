package logs

import (
	"context"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

const Name = "query_logs"

type Input struct {
	Service   string           `json:"service" jsonschema:"required,description=Service name"`
	TimeRange common.TimeRange `json:"time_range" jsonschema:"required,description=Time range to search"`
	Keywords  []string         `json:"keywords,omitempty" jsonschema:"description=Optional keywords to match"`
	Level     string           `json:"level,omitempty" jsonschema:"description=Optional log level,enum=debug,enum=info,enum=warn,enum=error"`
}

type MockTool struct {
	timeout time.Duration
}

func NewMockTool(timeout time.Duration) *MockTool {
	return &MockTool{timeout: timeout}
}

func (t *MockTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.Execute(ctx, common.ExecuteOptions{
		ToolName: Name,
		Timeout:  t.timeout,
		Fallback: "narrow the log time range or inspect the log backend directly",
	}, func(ctx context.Context) (common.ToolResult, error) {
		if err := ctx.Err(); err != nil {
			return common.ToolResult{}, err
		}
		if toolErr := validate(input); toolErr != nil {
			return common.ToolResult{}, toolErr
		}
		return mockResult(input), nil
	})
}

func mockResult(input Input) common.ToolResult {
	confidence := 0.92
	level := strings.ToLower(strings.TrimSpace(input.Level))
	if level == "" {
		level = "error"
	}
	return common.ToolResult{
		Evidence: []common.EvidenceItem{
			{
				ID:         "log-evidence-001",
				SourceType: "logs",
				SourceName: "mock-logs",
				TimeRange:  &input.TimeRange,
				Content:    "Mock logs show repeated upstream timeout errors for service " + strings.TrimSpace(input.Service) + ".",
				ResourceID: strings.TrimSpace(input.Service),
				Confidence: &confidence,
				Metadata: map[string]any{
					"level":    level,
					"keywords": input.Keywords,
				},
			},
		},
		Payload: map[string]any{
			"matched_entries": 18,
		},
		Metadata: map[string]any{"mode": "mock", "fallback_used": false},
	}
}

func validate(input Input) *common.ToolError {
	if strings.TrimSpace(input.Service) == "" {
		return common.InvalidArgument(Name, "service is required", map[string]any{"field": "service"})
	}
	if err := input.TimeRange.Validate(); err != nil {
		return common.InvalidArgument(Name, err.Error(), map[string]any{"field": "time_range"})
	}

	switch strings.ToLower(strings.TrimSpace(input.Level)) {
	case "", "debug", "info", "warn", "error":
		return nil
	default:
		return common.InvalidArgument(
			Name,
			"level must be one of debug, info, warn, or error",
			map[string]any{"field": "level"},
		)
	}
}
