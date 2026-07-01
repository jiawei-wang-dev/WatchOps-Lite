package metrics

import (
	"context"
	"strings"
	"time"

	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

const Name = "query_metrics"

type Input struct {
	Service    string           `json:"service" jsonschema:"required,description=Service name"`
	MetricName string           `json:"metric_name,omitempty" jsonschema:"description=Metric name"`
	Symptom    string           `json:"symptom,omitempty" jsonschema:"description=Observed symptom when a metric name is unknown"`
	TimeRange  common.TimeRange `json:"time_range" jsonschema:"required,description=Time range to query"`
}

type MockTool struct {
	runtime *toolruntime.Runtime
}

func NewMockTool(timeout time.Duration) *MockTool {
	return &MockTool{runtime: mustRuntime(toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceMetrics,
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
		return toolruntime.Result{}, invalidArgument("invalid metrics tool input", nil)
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
	confidence := 0.95
	service := strings.TrimSpace(input.Service)
	return toolruntime.Result{
		Evidence: []toolruntime.Evidence{
			{
				EvidenceID: "metric-evidence-001",
				SourceType: toolruntime.SourceMetrics,
				Source:     "mock-metrics",
				TimeRange: &toolruntime.TimeRange{
					From: input.TimeRange.From,
					To:   input.TimeRange.To,
				},
				Content:    "Mock metrics show p95 latency at 1.8s and an error rate of 6.2% for service " + service + ".",
				ResourceID: service,
				Confidence: &confidence,
				Metadata: map[string]any{
					"metric_name": input.MetricName,
					"symptom":     input.Symptom,
				},
			},
		},
		Payload: map[string]any{
			"p95_latency_ms": 1800,
			"error_rate":     0.062,
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
	if strings.TrimSpace(input.MetricName) == "" && strings.TrimSpace(input.Symptom) == "" {
		return invalidArgument(
			"metric_name or symptom is required",
			map[string]any{"fields": []string{"metric_name", "symptom"}},
		)
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
