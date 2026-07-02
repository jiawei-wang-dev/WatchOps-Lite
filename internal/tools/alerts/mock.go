package alerts

import (
	"context"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

const Name = "query_alerts"

type Input struct {
	Service  string `json:"service" jsonschema:"required,description=Service name"`
	Severity string `json:"severity,omitempty" jsonschema:"description=Optional alert severity filter"`
	Window   string `json:"window,omitempty" jsonschema:"description=Optional recent alert window, such as 30m or 1h"`
}

type MockTool struct {
	runtime *toolruntime.Runtime
}

func NewMockTool(timeout time.Duration) *MockTool {
	return &MockTool{runtime: mustRuntime(toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceAlerts,
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
		return toolruntime.Result{}, invalidArgument("invalid alerts tool input", nil)
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
	service := strings.TrimSpace(input.Service)
	severity := strings.ToLower(strings.TrimSpace(input.Severity))
	if severity == "" {
		severity = "warning"
	}
	window := strings.TrimSpace(input.Window)
	if window == "" {
		window = "30m"
	}
	confidence := 0.82
	return toolruntime.Result{
		Evidence: []evidence.Item{
			{
				ID:         "alert-evidence-001",
				Type:       evidence.TypeAlertSignal,
				Source:     evidence.SourceAlerts,
				SourceName: "mock-alerts",
				Content:    "Mock alert CheckoutHighErrorRate is firing for service " + service + " with severity " + severity + ".",
				ResourceID: service,
				Confidence: &confidence,
				Metadata: map[string]any{
					"alert_name": "CheckoutHighErrorRate",
					"service":    service,
					"severity":   severity,
					"status":     "firing",
					"window":     window,
					"summary":    "Checkout error rate is above the demo threshold.",
				},
			},
		},
		Payload: map[string]any{
			"active_alerts": 1,
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
	switch strings.ToLower(strings.TrimSpace(input.Severity)) {
	case "", "info", "warning", "critical", "page":
		return nil
	default:
		return invalidArgument(
			"severity must be one of info, warning, critical, or page",
			map[string]any{"field": "severity"},
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
