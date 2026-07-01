package logs

import (
	"context"
	"strings"
	"time"

	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
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
	runtime *toolruntime.Runtime
}

func NewMockTool(timeout time.Duration) *MockTool {
	return &MockTool{runtime: mustRuntime(toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceLogs,
		Timeout:    timeout,
		Operation:  mockOperation,
	})}
}

func (t *MockTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.ExecuteRuntime(ctx, t.runtime, input)
}

func mockOperation(ctx context.Context, value any) (toolruntime.Result, error) {
	input, ok := value.(Input)
	if !ok {
		return toolruntime.Result{}, toolruntime.NewToolError(
			toolruntime.ErrorCodeInvalidArgument,
			Name,
			"invalid log tool input",
			false,
			nil,
		)
	}
	if err := ctx.Err(); err != nil {
		return toolruntime.Result{}, err
	}
	if toolErr := validate(input); toolErr != nil {
		return toolruntime.Result{}, toolErr
	}
	return mockResult(input), nil
}

func mockResult(input Input) toolruntime.Result {
	confidence := 0.92
	level := strings.ToLower(strings.TrimSpace(input.Level))
	if level == "" {
		level = "error"
	}
	return toolruntime.Result{
		Evidence: []toolruntime.Evidence{
			{
				EvidenceID: "log-evidence-001",
				SourceType: toolruntime.SourceLogs,
				Source:     "mock-logs",
				TimeRange: &toolruntime.TimeRange{
					From: input.TimeRange.From,
					To:   input.TimeRange.To,
				},
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
		Metadata: map[string]any{"mode": "mock"},
	}
}

func validate(input Input) *toolruntime.ToolError {
	if strings.TrimSpace(input.Service) == "" {
		return invalidArgument("service is required", map[string]any{"field": "service"})
	}
	if err := input.TimeRange.Validate(); err != nil {
		return invalidArgument(err.Error(), map[string]any{"field": "time_range"})
	}

	switch strings.ToLower(strings.TrimSpace(input.Level)) {
	case "", "debug", "info", "warn", "error":
		return nil
	default:
		return invalidArgument(
			"level must be one of debug, info, warn, or error",
			map[string]any{"field": "level"},
		)
	}
}

func invalidArgument(message string, details map[string]any) *toolruntime.ToolError {
	return toolruntime.NewToolError(
		toolruntime.ErrorCodeInvalidArgument,
		Name,
		message,
		false,
		details,
	)
}

func mustRuntime(config toolruntime.Config) *toolruntime.Runtime {
	result, err := toolruntime.New(config)
	if err != nil {
		panic(err)
	}
	return result
}
