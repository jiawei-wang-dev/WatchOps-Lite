package knowledge

import (
	"context"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

const Name = "search_knowledge"

type Input struct {
	Query    string `json:"query" jsonschema:"required,description=Knowledge search query"`
	TopK     int    `json:"top_k" jsonschema:"required,description=Maximum number of results"`
	Category string `json:"category,omitempty" jsonschema:"description=Optional knowledge category"`
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
		Fallback: "retry with a more specific knowledge query",
	}, func(ctx context.Context) (common.ToolResult, error) {
		if err := ctx.Err(); err != nil {
			return common.ToolResult{}, err
		}
		if toolErr := validate(input); toolErr != nil {
			return common.ToolResult{}, toolErr
		}

		score := 0.88
		category := strings.TrimSpace(input.Category)
		if category == "" {
			category = "runbook"
		}

		return common.ToolResult{
			Evidence: []common.EvidenceItem{
				{
					ID:         "knowledge-evidence-001",
					SourceType: "knowledge",
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
		}, nil
	})
}

func validate(input Input) *common.ToolError {
	if strings.TrimSpace(input.Query) == "" {
		return common.InvalidArgument(Name, "query is required", map[string]any{"field": "query"})
	}
	if input.TopK < 1 || input.TopK > 10 {
		return common.InvalidArgument(
			Name,
			"top_k must be between 1 and 10",
			map[string]any{"field": "top_k", "minimum": 1, "maximum": 10},
		)
	}
	return nil
}
