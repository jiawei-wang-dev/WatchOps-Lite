package knowledge

import (
	"context"
	"errors"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/embedding"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/rerank"
)

type hybridStoreStub struct {
	storeStub
	vectorResults []SearchResult
	vectorErr     error
}

func (s *hybridStoreStub) SearchVector(
	context.Context,
	VectorSearchQuery,
) ([]SearchResult, error) {
	return s.vectorResults, s.vectorErr
}

func TestHybridRetrieveFusesBM25AndVectorResults(t *testing.T) {
	provider, _ := embedding.NewDeterministic(8)
	store := &hybridStoreStub{
		storeStub: storeStub{results: []SearchResult{
			{
				ChunkID: "shared", DocumentID: "doc-1", Title: "Checkout runbook",
				Content: "Inspect checkout timeout.", Source: "runbook",
				Score: 5, Metadata: map[string]any{"category": "runbook"},
			},
			{ChunkID: "bm25", Score: 4, Metadata: map[string]any{}},
		}},
		vectorResults: []SearchResult{
			{ChunkID: "shared", Score: 0.9, Metadata: map[string]any{}},
			{ChunkID: "vector", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	service, err := NewServiceWithConfig(store, provider, ServiceConfig{
		ChunkMaxSize:   100,
		RetrievalMode:  "hybrid",
		BM25TopK:       10,
		VectorTopK:     10,
		FinalTopK:      3,
		RRFK:           60,
		FallbackToBM25: true,
	})
	if err != nil {
		t.Fatalf("NewServiceWithConfig() error = %v", err)
	}

	result, err := service.HybridRetrieve(context.Background(), RetrievalRequest{
		Query: "checkout timeout",
		TopK:  3,
	})
	if err != nil {
		t.Fatalf("HybridRetrieve() error = %v", err)
	}
	if len(result.Chunks) != 3 ||
		result.Chunks[0].ChunkID != "shared" ||
		result.Chunks[0].FusedScore == 0 ||
		result.Metadata["retrieval_mode"] != "hybrid" {
		t.Fatalf("result = %#v", result)
	}
}

func TestHybridRetrieveFallbackToBM25(t *testing.T) {
	store := &hybridStoreStub{
		storeStub: storeStub{results: []SearchResult{{
			ChunkID: "bm25", DocumentID: "doc-bm25", Score: 4, Metadata: map[string]any{},
		}}},
		vectorErr: errors.New("vector unavailable"),
	}
	service, err := NewServiceWithConfig(store, nil, ServiceConfig{
		ChunkMaxSize:   100,
		RetrievalMode:  "hybrid",
		BM25TopK:       10,
		VectorTopK:     10,
		FinalTopK:      3,
		RRFK:           60,
		FallbackToBM25: true,
	})
	if err != nil {
		t.Fatalf("NewServiceWithConfig() error = %v", err)
	}

	result, err := service.HybridRetrieve(context.Background(), RetrievalRequest{
		Query: "checkout",
		TopK:  3,
	})
	if err != nil {
		t.Fatalf("HybridRetrieve() error = %v", err)
	}
	if len(result.Chunks) != 1 ||
		result.Chunks[0].RetrievalMethod != "bm25" ||
		result.Metadata["fallback_to_bm25"] != true {
		t.Fatalf("result = %#v", result)
	}
}

func TestServiceBM25ModeDoesNotRequireEmbedding(t *testing.T) {
	service, err := NewService(&storeStub{results: []SearchResult{{ChunkID: "bm25", Score: 2}}}, 100)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.HybridRetrieve(context.Background(), RetrievalRequest{Query: "checkout"})
	if err != nil || len(result.Chunks) != 1 || result.Chunks[0].RetrievalMethod != "bm25" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestServiceIndexesEmbeddingsWhenProviderIsEnabled(t *testing.T) {
	provider, _ := embedding.NewDeterministic(8)
	store := &hybridStoreStub{}
	service, err := NewServiceWithConfig(store, provider, ServiceConfig{
		ChunkMaxSize:   100,
		RetrievalMode:  "hybrid",
		BM25TopK:       10,
		VectorTopK:     10,
		FinalTopK:      5,
		RRFK:           60,
		FallbackToBM25: true,
	})
	if err != nil {
		t.Fatalf("NewServiceWithConfig() error = %v", err)
	}
	_, err = service.Ingest(context.Background(), Document{
		Title: "Runbook", Source: "test", Content: "Inspect checkout saturation.",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if len(store.indexed) != 1 || len(store.indexed[0].Embedding) != 8 {
		t.Fatalf("indexed chunks = %#v", store.indexed)
	}
}

func TestServiceReranksCandidateSetBeforeFinalTopK(t *testing.T) {
	store := &storeStub{results: []SearchResult{
		{
			ChunkID: "generic", DocumentID: "doc-generic", Title: "Operations",
			Content: "General reference.", Score: 1, Metadata: map[string]any{},
		},
		{
			ChunkID: "checkout", DocumentID: "doc-checkout", Title: "Checkout timeout runbook",
			Content: "Inspect payment timeout saturation.", Score: 1, Metadata: map[string]any{"service": "checkout"},
		},
	}}
	service, err := NewServiceWithReranker(
		store,
		nil,
		rerank.NewRuleBased(),
		ServiceConfig{
			ChunkMaxSize:     100,
			RetrievalMode:    "bm25",
			BM25TopK:         2,
			VectorTopK:       2,
			FinalTopK:        1,
			RRFK:             60,
			FallbackToBM25:   true,
			RerankCandidateK: 5,
			RerankTopK:       1,
		},
	)
	if err != nil {
		t.Fatalf("NewServiceWithReranker() error = %v", err)
	}
	result, err := service.HybridRetrieve(
		context.Background(),
		RetrievalRequest{Query: "checkout timeout runbook", TopK: 1},
	)
	if err != nil {
		t.Fatalf("HybridRetrieve() error = %v", err)
	}
	if store.query.Limit != 5 {
		t.Fatalf("candidate limit = %d, want 5", store.query.Limit)
	}
	if len(result.Chunks) != 1 ||
		result.Chunks[0].ChunkID != "checkout" ||
		result.Chunks[0].Metadata["rerank_provider"] != "rule_based" ||
		result.Chunks[0].Metadata["rerank_score"] == nil ||
		result.Chunks[0].Metadata["rerank_reason"] == nil {
		t.Fatalf("result = %#v", result)
	}
}
