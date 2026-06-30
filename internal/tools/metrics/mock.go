package metrics

import (
	"context"
	"strings"
	"time"

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
	timeout time.Duration
}

func NewMockTool(timeout time.Duration) *MockTool {
	return &MockTool{timeout: timeout}
}

func (t *MockTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.Execute(ctx, common.ExecuteOptions{
		ToolName: Name,
		Timeout:  t.timeout,
		Fallback: "narrow the metric time range or inspect the metrics backend directly",
	}, func(ctx context.Context) (common.ToolResult, error) {
		if err := ctx.Err(); err != nil {
			return common.ToolResult{}, err
		}
		if toolErr := validate(input); toolErr != nil {
			return common.ToolResult{}, toolErr
		}

		confidence := 0.95
		service := strings.TrimSpace(input.Service)
		return common.ToolResult{
			Evidence: []common.EvidenceItem{
				{
					ID:         "metric-evidence-001",
					SourceType: "metrics",
					SourceName: "mock-metrics",
					TimeRange:  &input.TimeRange,
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
			Metadata: map[string]any{"mode": "mock"},
		}, nil
	})
}

func validate(input Input) *common.ToolError {
	if strings.TrimSpace(input.Service) == "" {
		return common.InvalidArgument(Name, "service is required", map[string]any{"field": "service"})
	}
	if strings.TrimSpace(input.MetricName) == "" && strings.TrimSpace(input.Symptom) == "" {
		return common.InvalidArgument(
			Name,
			"metric_name or symptom is required",
			map[string]any{"fields": []string{"metric_name", "symptom"}},
		)
	}
	if err := input.TimeRange.Validate(); err != nil {
		return common.InvalidArgument(Name, err.Error(), map[string]any{"field": "time_range"})
	}
	return nil
}
