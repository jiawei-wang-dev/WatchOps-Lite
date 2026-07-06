package queryplan

import (
	"testing"

	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
)

func TestMergeResultsDedupesByChunkID(t *testing.T) {
	plan := RAGQueryPlan{
		Source: "rule",
		Queries: []RAGSubQuery{
			{Type: QueryOriginal, Query: "checkout 500", Weight: 1},
			{Type: QueryCanonical, Query: "checkout-service HTTP 5xx", Weight: 0.9},
		},
		Metadata: map[string]any{"query_plan_fallback_used": false},
	}

	result := MergeResults(plan, []RAGSubQueryResult{
		{
			Query: plan.Queries[0],
			Result: retrievalknowledge.RetrievalResult{Chunks: []retrievalknowledge.RetrievedKnowledge{{
				ID: "a", ChunkID: "chunk-1", DocumentID: "doc-1",
				Content: "checkout timeout runbook", Score: 0.7,
			}}},
		},
		{
			Query: plan.Queries[1],
			Result: retrievalknowledge.RetrievalResult{Chunks: []retrievalknowledge.RetrievedKnowledge{{
				ID: "b", ChunkID: "chunk-1", DocumentID: "doc-1",
				Content: "duplicate checkout timeout runbook", Score: 0.95,
			}}},
		},
	}, 5)

	if len(result.Chunks) != 1 {
		t.Fatalf("chunks = %#v, want one deduped chunk", result.Chunks)
	}
	matched, _ := result.Chunks[0].Metadata["rag_matched_sub_queries"].([]string)
	if len(matched) != 2 {
		t.Fatalf("metadata = %#v, want both sub-query matches", result.Chunks[0].Metadata)
	}
	if result.Metadata["query_rewrite_applied"] != true ||
		result.Metadata["rag_sub_query_count"] != 2 ||
		result.Metadata["selected_chunk_count"] != 1 {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
}
