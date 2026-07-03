package knowledge

import (
	"context"
	"errors"
	"testing"

	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
)

type searcherStub struct {
	results []retrievalknowledge.SearchResult
	err     error
}

type queryCapturingSearcher struct {
	query retrievalknowledge.SearchQuery
}

func (s *queryCapturingSearcher) Search(
	_ context.Context,
	query retrievalknowledge.SearchQuery,
) ([]retrievalknowledge.SearchResult, error) {
	s.query = query
	return []retrievalknowledge.SearchResult{{
		ChunkID:       "checkout_runbook_zh_chunk_0000",
		DocumentID:    "checkout_runbook_zh",
		Title:         "Checkout 服务高错误率排障 Runbook",
		Content:       "检查 payment 支付依赖延迟、超时和重试放大。",
		Source:        "watchops-lite-demo",
		Score:         4.2,
		RetrievalMode: "bm25",
	}}, nil
}

func (s searcherStub) Search(context.Context, retrievalknowledge.SearchQuery) ([]retrievalknowledge.SearchResult, error) {
	return s.results, s.err
}

func TestSearchToolReturnsElasticsearchEvidence(t *testing.T) {
	bm25Score := 3.2
	tool := NewSearchTool(searcherStub{results: []retrievalknowledge.SearchResult{{
		ChunkID: "chunk_1", DocumentID: "doc_1", Title: "Runbook",
		Content: "Inspect saturation.", Source: "manual", Score: 3.2,
		RetrievalMode: "bm25", BM25Score: &bm25Score,
	}}}, 0)

	result, err := tool.Execute(context.Background(), Input{Query: "latency", TopK: 3})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Metadata["mode"] != "elasticsearch" || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Evidence[0].Metadata["retrieval_mode"] != "bm25" ||
		result.Evidence[0].Metadata["document_id"] != "doc_1" {
		t.Fatalf("evidence metadata = %#v", result.Evidence[0].Metadata)
	}
}

func TestSearchToolReportsVectorFallback(t *testing.T) {
	tool := NewSearchTool(searcherStub{results: []retrievalknowledge.SearchResult{{
		ChunkID: "chunk_1", DocumentID: "doc_1", Title: "Runbook",
		Content: "Inspect saturation.", Source: "manual", Score: 3.2,
		RetrievalMode: "bm25",
		Metadata:      map[string]any{"vector_fallback": true},
	}}}, 0)

	result, err := tool.Execute(context.Background(), Input{Query: "latency", TopK: 3})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Warnings) != 1 ||
		result.Warnings[0].Code != "KNOWLEDGE_VECTOR_FALLBACK" ||
		result.Metadata["fallback_used"] != true {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchToolMapsRerankMetadataAndFallback(t *testing.T) {
	tool := NewSearchTool(searcherStub{results: []retrievalknowledge.SearchResult{{
		ChunkID: "chunk_1", DocumentID: "doc_1", Title: "Runbook",
		Content: "Inspect saturation.", Source: "manual", Score: 4.5,
		RetrievalMode: "bm25",
		Metadata: map[string]any{
			"rerank_provider":        "rule_based",
			"rerank_score":           4.5,
			"rerank_reason":          "title_overlap",
			"rerank_fallback_reason": "external_unavailable",
		},
	}}}, 0)

	result, err := tool.Execute(context.Background(), Input{Query: "latency", TopK: 3})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Warnings) != 1 ||
		result.Warnings[0].Code != "KNOWLEDGE_RERANK_FALLBACK" ||
		result.Metadata["rerank_provider"] != "rule_based" ||
		result.Metadata["fallback_used"] != true ||
		result.Evidence[0].Metadata["rerank_score"] != 4.5 {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchToolFallsBackToMockEvidence(t *testing.T) {
	tool := NewSearchTool(searcherStub{err: errors.New("connection refused")}, 0)

	result, err := tool.Execute(context.Background(), Input{Query: "latency", TopK: 3})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Metadata["mode"] != "mock_fallback" || len(result.Warnings) != 1 {
		t.Fatalf("result = %#v, want explicit fallback", result)
	}
}

func TestSearchToolPreservesChineseQueryAndRunbookEvidence(t *testing.T) {
	searcher := &queryCapturingSearcher{}
	tool := NewSearchTool(searcher, 0)

	result, err := tool.Execute(context.Background(), Input{
		Query: "checkout 支付超时怎么排查",
		TopK:  3,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if searcher.query.Query != "checkout 支付超时怎么排查" {
		t.Fatalf("search query = %q", searcher.query.Query)
	}
	if len(result.Evidence) != 1 ||
		result.Evidence[0].ID != "checkout_runbook_zh_chunk_0000" ||
		result.Evidence[0].Content != "检查 payment 支付依赖延迟、超时和重试放大。" {
		t.Fatalf("Chinese evidence = %#v", result.Evidence)
	}
}
