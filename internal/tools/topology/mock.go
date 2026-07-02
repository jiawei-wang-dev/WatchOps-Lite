package topology

import (
	"context"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

const Name = "get_service_topology"

type Input struct {
	Service string `json:"service" jsonschema:"required,description=Service name"`
	Depth   int    `json:"depth,omitempty" jsonschema:"description=Optional dependency depth"`
}

type MockTool struct {
	runtime *toolruntime.Runtime
}

type serviceTopology struct {
	upstream     []string
	downstream   []string
	dependencies []string
	riskNotes    []string
}

func NewMockTool(timeout time.Duration) *MockTool {
	return &MockTool{runtime: mustRuntime(toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceTopology,
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
		return toolruntime.Result{}, invalidArgument("invalid topology tool input", nil)
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
	service := strings.ToLower(strings.TrimSpace(input.Service))
	topology := demoTopology(service)
	confidence := 0.88
	return toolruntime.Result{
		Evidence: []evidence.Item{
			{
				ID:         "topology-evidence-" + service,
				Type:       evidence.TypeTopology,
				Source:     evidence.SourceTopology,
				SourceName: "demo-topology",
				Content:    topologySummary(service, topology),
				ResourceID: service,
				Confidence: &confidence,
				Metadata: map[string]any{
					"service":             service,
					"upstream_services":   topology.upstream,
					"downstream_services": topology.downstream,
					"dependencies":        topology.dependencies,
					"risk_notes":          topology.riskNotes,
					"depth":               normalizedDepth(input.Depth),
				},
			},
		},
		Payload: map[string]any{
			"service":             service,
			"upstream_services":   topology.upstream,
			"downstream_services": topology.downstream,
			"dependencies":        topology.dependencies,
			"risk_notes":          topology.riskNotes,
		},
		Metadata: map[string]any{
			"mode":    "mock",
			"backend": "demo_topology",
		},
	}
}

func demoTopology(service string) serviceTopology {
	switch service {
	case "checkout":
		return serviceTopology{
			upstream:     []string{"web", "cart"},
			downstream:   []string{"payment", "order", "inventory"},
			dependencies: []string{"payment", "order", "inventory", "redis", "mysql"},
			riskNotes: []string{
				"payment latency can amplify checkout retries",
				"order write failures can surface as checkout errors",
			},
		}
	case "payment":
		return serviceTopology{
			upstream:     []string{"checkout"},
			downstream:   []string{"bank-gateway", "fraud"},
			dependencies: []string{"bank-gateway", "fraud", "mysql"},
			riskNotes:    []string{"bank-gateway latency is a common checkout bottleneck"},
		}
	default:
		return serviceTopology{
			upstream:     []string{},
			downstream:   []string{},
			dependencies: []string{},
			riskNotes:    []string{"no detailed demo topology is configured for this service"},
		}
	}
}

func topologySummary(service string, topology serviceTopology) string {
	if len(topology.dependencies) == 0 {
		return "Demo topology has no configured dependencies for service " + service + "."
	}
	return "Demo topology for service " + service + " includes dependencies " + strings.Join(topology.dependencies, ", ") + "."
}

func validate(input Input) *toolruntime.ToolError {
	if strings.TrimSpace(input.Service) == "" {
		return invalidArgument("service is required", map[string]any{"field": "service"})
	}
	if input.Depth < 0 || input.Depth > 3 {
		return invalidArgument("depth must be between 0 and 3", map[string]any{"field": "depth"})
	}
	return nil
}

func normalizedDepth(depth int) int {
	if depth <= 0 {
		return 1
	}
	return depth
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
