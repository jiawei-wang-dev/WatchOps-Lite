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
	HybridRetrieve(context.Context, retrievalknowledge.RetrievalRequest) (retrievalknowledge.RetrievalResult, error)
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
	result, err := t.searcher.HybridRetrieve(ctx, retrievalknowledge.RetrievalRequest{
		Query:   input.Query,
		TopK:    input.TopK,
		Filters: filters,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return toolruntime.Result{}, err
		}
		return toolruntime.Result{}, dependencyUnavailable()
	}

	evidenceItems := make([]evidence.Item, 0, len(result.Chunks))
	warnings := []toolruntime.Warning{}
	retrievalMode := metadataString(result.Metadata, "retrieval_mode", "bm25")
	vectorFallback := metadataBool(result.Metadata, "fallback_to_bm25")
	rerankProvider := ""
	rerankFallbackReason := ""
	for _, chunk := range result.Chunks {
		if chunk.RetrievalMethod != "" {
			retrievalMode = chunk.RetrievalMethod
		}
		if fallback, _ := chunk.Metadata["vector_fallback"].(bool); fallback {
			vectorFallback = true
		}
		if provider, _ := chunk.Metadata["rerank_provider"].(string); provider != "" {
			rerankProvider = provider
		}
		if reason, _ := chunk.Metadata["rerank_fallback_reason"].(string); reason != "" {
			rerankFallbackReason = reason
		}
		score := chunk.Score
		evidenceItems = append(evidenceItems, evidence.Item{
			ID:         chunk.ID,
			Type:       evidence.TypeKnowledgeChunk,
			Source:     evidence.SourceKnowledge,
			SourceName: chunk.Source,
			Content:    chunk.Content,
			ResourceID: chunk.DocumentID,
			Score:      &score,
			Metadata: map[string]any{
				"title":                  chunk.Title,
				"chunk_id":               chunk.ChunkID,
				"document_id":            chunk.DocumentID,
				"retrieval_mode":         chunk.RetrievalMethod,
				"bm25_score":             chunk.BM25Score,
				"vector_score":           chunk.VectorScore,
				"rrf_score":              chunk.FusedScore,
				"rerank_provider":        chunk.Metadata["rerank_provider"],
				"rerank_score":           chunk.RerankScore,
				"rerank_reason":          chunk.Metadata["rerank_reason"],
				"rerank_fallback_reason": chunk.Metadata["rerank_fallback_reason"],
				"metadata":               chunk.Metadata,
			},
		})
	}
	if vectorFallback {
		warnings = append(warnings, toolruntime.Warning{
			Code:    "KNOWLEDGE_VECTOR_FALLBACK",
			Message: "Vector retrieval was unavailable; BM25 knowledge evidence was returned.",
		})
	}
	if rerankFallbackReason != "" {
		warnings = append(warnings, toolruntime.Warning{
			Code:    "KNOWLEDGE_RERANK_FALLBACK",
			Message: "External knowledge reranking was unavailable; deterministic rule-based ordering was used.",
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
			"mode":                   "elasticsearch",
			"retrieval_mode":         retrievalMode,
			"rerank_provider":        rerankProvider,
			"rerank_fallback_reason": rerankFallbackReason,
			"retrieval_metadata":     result.Metadata,
			"fallback_used":          vectorFallback || rerankFallbackReason != "",
		},
	}, nil
}

func metadataString(metadata map[string]any, key string, fallback string) string {
	if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func metadataBool(metadata map[string]any, key string) bool {
	value, _ := metadata[key].(bool)
	return value
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
