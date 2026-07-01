package knowledge

import (
	"context"
	"errors"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/embedding"
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

func TestServiceHybridSearchFusesBM25AndVectorResults(t *testing.T) {
	provider, _ := embedding.NewDeterministic(8)
	store := &hybridStoreStub{
		storeStub: storeStub{results: []SearchResult{
			{ChunkID: "shared", Score: 5, Metadata: map[string]any{}},
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

	results, err := service.Search(context.Background(), SearchQuery{Query: "checkout", Limit: 3})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 3 ||
		results[0].ChunkID != "shared" ||
		results[0].RetrievalMode != "hybrid" ||
		results[0].RRFScore == nil {
		t.Fatalf("results = %#v", results)
	}
}

func TestServiceHybridSearchFallsBackToBM25(t *testing.T) {
	store := &hybridStoreStub{
		storeStub: storeStub{results: []SearchResult{{
			ChunkID: "bm25", Score: 4, Metadata: map[string]any{},
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

	results, err := service.Search(context.Background(), SearchQuery{Query: "checkout"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 ||
		results[0].RetrievalMode != "bm25" ||
		results[0].Metadata["vector_fallback"] != true {
		t.Fatalf("results = %#v", results)
	}
}

func TestServiceBM25ModeDoesNotRequireEmbedding(t *testing.T) {
	service, err := NewService(&storeStub{results: []SearchResult{{ChunkID: "bm25", Score: 2}}}, 100)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	results, err := service.Search(context.Background(), SearchQuery{Query: "checkout"})
	if err != nil || len(results) != 1 || results[0].RetrievalMode != "bm25" {
		t.Fatalf("results=%#v error=%v", results, err)
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
	service.newID = func() (string, error) { return "doc_embed", nil }

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
