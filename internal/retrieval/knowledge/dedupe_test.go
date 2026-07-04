package knowledge

import "testing"

func TestDedupeSearchResultsUsesChunkIDAndKeepsHighestScore(t *testing.T) {
	results := dedupeSearchResults([]SearchResult{
		{ChunkID: "chunk-1", DocumentID: "doc-old", Score: 1},
		{ChunkID: "chunk-1", DocumentID: "doc-new", Score: 3},
	})
	if len(results) != 1 || results[0].DocumentID != "doc-new" {
		t.Fatalf("results = %#v", results)
	}
	assertDuplicateCount(t, results[0], 1)
}

func TestDedupeSearchResultsUsesContentHashAcrossDocuments(t *testing.T) {
	results := dedupeSearchResults([]SearchResult{
		{
			ChunkID: "old-0", DocumentID: "old", ChunkIndex: 0,
			Title: "Runbook", Content: "Check payment timeout.", Score: 2,
			Metadata: map[string]any{"content_hash": "same"},
		},
		{
			ChunkID: "new-0", DocumentID: "new", ChunkIndex: 0,
			Title: "Runbook", Content: "Check payment timeout.", Score: 4,
			Metadata: map[string]any{
				"content_hash": "same",
				"rerank_score": float64(8),
			},
		},
	})
	if len(results) != 1 || results[0].DocumentID != "new" {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Metadata["dedupe_reason"] != "content_hash" {
		t.Fatalf("metadata = %#v", results[0].Metadata)
	}
}

func TestDedupeSearchResultsHandlesLegacyChineseDuplicates(t *testing.T) {
	results := dedupeSearchResults([]SearchResult{
		{
			ChunkID: "zh-1", DocumentID: "doc-1",
			Title:   "Checkout 服务高错误率排障 Runbook",
			Content: "检查 payment 支付依赖超时和重试放大。",
			Score:   5,
		},
		{
			ChunkID: "zh-2", DocumentID: "doc-2",
			Title:   "## checkout 服务高错误率排障 runbook",
			Content: "检查  payment 支付依赖超时和重试放大。",
			Score:   4,
		},
	})
	if len(results) != 1 || results[0].ChunkID != "zh-1" {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Metadata["dedupe_reason"] != "content_fingerprint" {
		t.Fatalf("metadata = %#v", results[0].Metadata)
	}
}

func TestDedupeSearchResultsKeepsSameTitleWithDifferentContent(t *testing.T) {
	results := dedupeSearchResults([]SearchResult{
		{
			ChunkID: "payment", DocumentID: "doc-payment",
			Title: "Checkout Runbook", Content: "Check payment latency.",
			Score: 2,
		},
		{
			ChunkID: "redis", DocumentID: "doc-redis",
			Title: "Checkout Runbook", Content: "Check Redis saturation.",
			Score: 1,
		},
	})
	if len(results) != 2 {
		t.Fatalf("same title with different content was deduped: %#v", results)
	}
}

func assertDuplicateCount(t *testing.T, result SearchResult, want int) {
	t.Helper()
	count, ok := result.Metadata["deduped_duplicate_count"].(int)
	if !ok || count != want {
		t.Fatalf("deduped_duplicate_count = %#v, want %d", result.Metadata, want)
	}
}
