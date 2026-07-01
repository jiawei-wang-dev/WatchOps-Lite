package knowledge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type Searcher interface {
	Search(context.Context, retrievalknowledge.SearchQuery) ([]retrievalknowledge.SearchResult, error)
}

type SearchTool struct {
	searcher Searcher
	runtime  *toolruntime.Runtime
}

func NewSearchTool(searcher Searcher, timeout time.Duration) *SearchTool {
	tool := &SearchTool{searcher: searcher}
	tool.runtime = mustRuntime(toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceKnowledge,
		Timeout:    timeout,
		Operation: func(ctx context.Context, value any) (toolruntime.Result, error) {
			input, ok := value.(Input)
			if !ok {
				return toolruntime.Result{}, invalidArgument("invalid knowledge tool input", nil)
			}
			return tool.search(ctx, input)
		},
		Fallback: mockOperation,
		FallbackWarning: toolruntime.Warning{
			Code:    "KNOWLEDGE_FALLBACK",
			Message: "Elasticsearch knowledge retrieval was unavailable; mock evidence was returned.",
		},
		FallbackMetadata: map[string]any{
			"mode": "mock_fallback",
		},
	})
	return tool
}

func (t *SearchTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.ExecuteRuntime(ctx, t.runtime, input)
}

func (t *SearchTool) Runtime() *toolruntime.Runtime {
	return t.runtime
}

func (t *SearchTool) search(ctx context.Context, input Input) (toolruntime.Result, error) {
	if toolErr := validate(input); toolErr != nil {
		return toolruntime.Result{}, toolErr
	}

	filters := map[string]string{}
	if category := strings.TrimSpace(input.Category); category != "" {
		filters["category"] = category
	}
	if t.searcher == nil {
		return toolruntime.Result{}, dependencyUnavailable()
	}
	results, err := t.searcher.Search(ctx, retrievalknowledge.SearchQuery{
		Query:   input.Query,
		Limit:   input.TopK,
		Filters: filters,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return toolruntime.Result{}, err
		}
		return toolruntime.Result{}, dependencyUnavailable()
	}

	evidenceItems := make([]evidence.Item, 0, len(results))
	warnings := []toolruntime.Warning{}
	retrievalMode := "bm25"
	vectorFallback := false
	for _, result := range results {
		if result.RetrievalMode != "" {
			retrievalMode = result.RetrievalMode
		}
		if fallback, _ := result.Metadata["vector_fallback"].(bool); fallback {
			vectorFallback = true
		}
		score := result.Score
		evidenceItems = append(evidenceItems, evidence.Item{
			ID:         result.ChunkID,
			Type:       evidence.TypeKnowledgeChunk,
			Source:     evidence.SourceKnowledge,
			SourceName: result.Source,
			Content:    result.Content,
			ResourceID: result.DocumentID,
			Score:      &score,
			Metadata: map[string]any{
				"title":          result.Title,
				"chunk_id":       result.ChunkID,
				"document_id":    result.DocumentID,
				"retrieval_mode": result.RetrievalMode,
				"bm25_score":     result.BM25Score,
				"vector_score":   result.VectorScore,
				"rrf_score":      result.RRFScore,
				"metadata":       result.Metadata,
			},
		})
	}
	if vectorFallback {
		warnings = append(warnings, toolruntime.Warning{
			Code:    "KNOWLEDGE_VECTOR_FALLBACK",
			Message: "Vector retrieval was unavailable; BM25 knowledge evidence was returned.",
		})
	}
	return toolruntime.Result{
		Evidence: evidenceItems,
		Warnings: warnings,
		Payload: map[string]any{
			"query":          strings.TrimSpace(input.Query),
			"returned_count": len(evidenceItems),
		},
		Metadata: map[string]any{
			"mode":           "elasticsearch",
			"retrieval_mode": retrievalMode,
			"fallback_used":  vectorFallback,
		},
	}, nil
}

func dependencyUnavailable() *toolruntime.ToolError {
	return toolruntime.NewToolError(
		toolruntime.ErrorCodeDependencyUnavailable,
		Name,
		"Elasticsearch knowledge backend is unavailable",
		true,
		map[string]any{"backend": "elasticsearch"},
	)
}
