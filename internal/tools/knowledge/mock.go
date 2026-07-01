package knowledge

import (
	"context"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

const Name = "search_knowledge"

type Input struct {
	Query    string `json:"query" jsonschema:"required,description=Knowledge search query"`
	TopK     int    `json:"top_k" jsonschema:"required,description=Maximum number of results"`
	Category string `json:"category,omitempty" jsonschema:"description=Optional knowledge category"`
}

type MockTool struct {
	runtime *toolruntime.Runtime
}

func NewMockTool(timeout time.Duration) *MockTool {
	return &MockTool{runtime: mustRuntime(toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceKnowledge,
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
		return toolruntime.Result{}, invalidArgument("invalid knowledge tool input", nil)
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
	score := 0.88
	category := strings.TrimSpace(input.Category)
	if category == "" {
		category = "runbook"
	}
	return toolruntime.Result{
		Evidence: []evidence.Item{
			{
				ID:         "knowledge-evidence-001",
				Type:       evidence.TypeKnowledgeChunk,
				Source:     evidence.SourceKnowledge,
				SourceName: "mock-runbooks",
				Content:    "Mock runbook recommends checking upstream timeout saturation before increasing client retry limits.",
				ResourceID: "runbook-checkout-timeouts",
				Score:      &score,
				Metadata: map[string]any{
					"category": category,
					"section":  "Initial diagnosis",
				},
			},
		},
		Payload: map[string]any{
			"query":          strings.TrimSpace(input.Query),
			"returned_count": 1,
		},
		Metadata: map[string]any{"mode": "mock"},
	}
}

func validate(input Input) *toolruntime.ToolError {
	if strings.TrimSpace(input.Query) == "" {
		return invalidArgument("query is required", map[string]any{"field": "query"})
	}
	if input.TopK < 1 || input.TopK > 10 {
		return invalidArgument(
			"top_k must be between 1 and 10",
			map[string]any{"field": "top_k", "minimum": 1, "maximum": 10},
		)
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
