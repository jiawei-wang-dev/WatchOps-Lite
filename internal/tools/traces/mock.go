package traces

import (
	"context"
	"strings"
	"time"

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
	timeout time.Duration
}

func NewMockTool(timeout time.Duration) *MockTool {
	return &MockTool{timeout: timeout}
}

func (t *MockTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.Execute(ctx, common.ExecuteOptions{
		ToolName: Name,
		Timeout:  t.timeout,
		Fallback: "narrow the trace time range or inspect the tracing backend directly",
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
	traceID := strings.TrimSpace(input.TraceID)
	if traceID == "" {
		traceID = "mock-trace-001"
	}
	confidence := 0.9
	return common.ToolResult{
		Evidence: []common.EvidenceItem{
			{
				ID:         "trace-evidence-001",
				SourceType: "traces",
				SourceName: "mock-traces",
				TimeRange:  &input.TimeRange,
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
			"mode":          "mock",
			"backend":       "mock",
			"fallback_used": false,
		},
	}
}

func validate(input Input) *common.ToolError {
	if strings.TrimSpace(input.Service) == "" {
		return common.InvalidArgument(Name, "service is required", map[string]any{"field": "service"})
	}
	if err := input.TimeRange.Validate(); err != nil {
		return common.InvalidArgument(Name, err.Error(), map[string]any{"field": "time_range"})
	}
	return nil
}
