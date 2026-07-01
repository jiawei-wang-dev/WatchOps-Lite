package traces

import (
	"context"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

const Name = "query_traces"

type Input struct {
	Service   string           `json:"service" jsonschema:"required,description=Service name"`
	TimeRange common.TimeRange `json:"time_range" jsonschema:"required,description=Time range to search"`
	TraceID   string           `json:"trace_id,omitempty" jsonschema:"description=Optional trace identifier"`
	Operation string           `json:"operation,omitempty" jsonschema:"description=Optional operation name filter"`
}

type MockTool struct {
	runtime *toolruntime.Runtime
}

func NewMockTool(timeout time.Duration) *MockTool {
	return &MockTool{runtime: mustRuntime(toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceTraces,
		Timeout:    timeout,
		Operation:  mockOperation,
	})}
}

func (t *MockTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.ExecuteRuntime(ctx, t.runtime, input)
}

func (t *MockTool) Runtime() *toolruntime.Runtime {
	return t.runtime
}

func mockOperation(ctx context.Context, value any) (toolruntime.Result, error) {
	input, ok := value.(Input)
	if !ok {
		return toolruntime.Result{}, invalidArgument("invalid trace tool input", nil)
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
	traceID := strings.TrimSpace(input.TraceID)
	if traceID == "" {
		traceID = "mock-trace-001"
	}
	confidence := 0.9
	return toolruntime.Result{
		Evidence: []evidence.Item{
			{
				ID:         "trace-evidence-001",
				Type:       evidence.TypeTraceSpan,
				Source:     evidence.SourceTraces,
				SourceName: "mock-traces",
				TimeRange: &evidence.TimeRange{
					From: input.TimeRange.From,
					To:   input.TimeRange.To,
				},
				TraceID:    traceID,
				Content:    "Mock trace shows span db.checkout taking 1.4s, accounting for most request latency.",
				ResourceID: traceID,
				Confidence: &confidence,
				Metadata: map[string]any{
					"service":   strings.TrimSpace(input.Service),
					"span_name": "db.checkout",
				},
			},
		},
		Payload: map[string]any{
			"trace_id":       traceID,
			"slow_span_ms":   1400,
			"total_trace_ms": 1650,
		},
		Metadata: map[string]any{
			"mode":    "mock",
			"backend": "mock",
		},
	}
}

func validate(input Input) *toolruntime.ToolError {
	if strings.TrimSpace(input.Service) == "" {
		return invalidArgument("service is required", map[string]any{"field": "service"})
	}
	if err := input.TimeRange.Validate(); err != nil {
		return invalidArgument(err.Error(), map[string]any{"field": "time_range"})
	}
	return nil
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
