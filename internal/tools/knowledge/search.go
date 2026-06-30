package knowledge

import (
	"context"
	"errors"
	"strings"
	"time"

	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type Searcher interface {
	Search(context.Context, retrievalknowledge.SearchQuery) ([]retrievalknowledge.SearchResult, error)
}

type SearchTool struct {
	searcher Searcher
	timeout  time.Duration
	fallback *MockTool
}

func NewSearchTool(searcher Searcher, timeout time.Duration) *SearchTool {
	return &SearchTool{
		searcher: searcher,
		timeout:  timeout,
		fallback: NewMockTool(timeout),
	}
}

func (t *SearchTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.Execute(ctx, common.ExecuteOptions{
		ToolName: Name,
		Timeout:  t.timeout,
		Fallback: "use mock knowledge evidence while Elasticsearch recovers",
	}, func(ctx context.Context) (common.ToolResult, error) {
		if toolErr := validate(input); toolErr != nil {
			return common.ToolResult{}, toolErr
		}

		filters := map[string]string{}
		if category := strings.TrimSpace(input.Category); category != "" {
			filters["category"] = category
		}
		results, err := t.searcher.Search(ctx, retrievalknowledge.SearchQuery{
			Query:   input.Query,
			Limit:   input.TopK,
			Filters: filters,
		})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return common.ToolResult{}, err
			}
			fallbackResult, fallbackErr := t.fallback.Execute(ctx, input)
			if fallbackErr != nil {
				return common.ToolResult{}, fallbackErr
			}
			fallbackResult.Warnings = append(fallbackResult.Warnings, common.ToolWarning{
				Code:    "KNOWLEDGE_FALLBACK",
				Message: "Elasticsearch knowledge retrieval was unavailable; mock evidence was returned.",
			})
			fallbackResult.Metadata["mode"] = "mock_fallback"
			return fallbackResult, nil
		}

		evidence := make([]common.EvidenceItem, 0, len(results))
		for _, result := range results {
			score := result.Score
			evidence = append(evidence, common.EvidenceItem{
				ID:         result.ChunkID,
				SourceType: "knowledge",
				SourceName: result.Source,
				Content:    result.Content,
				ResourceID: result.DocumentID,
				Score:      &score,
				Metadata: map[string]any{
					"title":    result.Title,
					"chunk_id": result.ChunkID,
					"metadata": result.Metadata,
				},
			})
		}
		return common.ToolResult{
			Evidence: evidence,
			Payload: map[string]any{
				"query":          strings.TrimSpace(input.Query),
				"returned_count": len(evidence),
			},
			Metadata: map[string]any{"mode": "elasticsearch"},
		}, nil
	})
}
