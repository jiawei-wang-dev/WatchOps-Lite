package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"
)

type storeStub struct {
	indexed   []Chunk
	results   []SearchResult
	info      DocumentInfo
	duplicate DuplicateDocument
	err       error
	query     SearchQuery
}

func (s *storeStub) EnsureIndex(context.Context) error { return s.err }
func (s *storeStub) IndexChunks(_ context.Context, chunks []Chunk) error {
	s.indexed = chunks
	return s.err
}
func (s *storeStub) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	s.query = query
	return s.results, s.err
}
func (s *storeStub) GetDocument(context.Context, string) (DocumentInfo, error) {
	return s.info, s.err
}
func (s *storeStub) FindByContentHash(context.Context, string) (DuplicateDocument, error) {
	if s.duplicate.DocumentID == "" {
		return DuplicateDocument{}, ErrNotFound
	}
	return s.duplicate, nil
}

func TestServiceIngestsDeterministicChunks(t *testing.T) {
	store := &storeStub{}
	service, err := NewService(store, 20)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

	result, err := service.Ingest(context.Background(), Document{
		Title:   "Checkout runbook",
		Source:  "runbook",
		Content: "Inspect latency.\n\nInspect retries and saturation.",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	wantDocumentID := documentIDFromContentHash(ContentHash(
		"Inspect latency.\n\nInspect retries and saturation.",
	))
	if result.DocumentID != wantDocumentID ||
		result.ChunkCount != len(store.indexed) {
		t.Fatalf("result = %#v, indexed=%d", result, len(store.indexed))
	}
	if store.indexed[0].ID != wantDocumentID+"_chunk_0000" {
		t.Fatalf("chunk ID = %q", store.indexed[0].ID)
	}
	if result.Status != "seeded" ||
		store.indexed[0].ContentHash == "" ||
		store.indexed[0].Metadata["content_hash"] == "" {
		t.Fatalf("dedupe metadata missing: result=%#v chunk=%#v", result, store.indexed[0])
	}
}

func TestServiceSkipsDuplicateContentBeforeChunking(t *testing.T) {
	store := &storeStub{duplicate: DuplicateDocument{
		DocumentID: "doc_existing",
		ChunkCount: 2,
	}}
	service, _ := NewService(store, 20)
	result, err := service.Ingest(context.Background(), Document{
		Title:   "Checkout runbook",
		Source:  "runbook",
		Content: "检查 payment 支付依赖超时。",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if result.DocumentID != "doc_existing" ||
		result.ChunkCount != 2 ||
		result.Status != "skipped_duplicate" {
		t.Fatalf("result = %#v", result)
	}
	if len(store.indexed) != 0 {
		t.Fatalf("duplicate ingestion indexed %d chunks", len(store.indexed))
	}
}

func TestServiceValidatesSearchAndPropagatesUnavailable(t *testing.T) {
	service, _ := NewService(&storeStub{err: ErrUnavailable}, 100)

	if _, err := service.HybridRetrieve(context.Background(), RetrievalRequest{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty retrieval error = %v, want ErrInvalidArgument", err)
	}
	if _, err := service.HybridRetrieve(context.Background(), RetrievalRequest{Query: "timeouts"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("retrieval error = %v, want ErrUnavailable", err)
	}
}

func TestServiceDeduplicatesHistoricalSearchResults(t *testing.T) {
	store := &storeStub{results: []SearchResult{
		{
			ChunkID: "old", DocumentID: "doc-old",
			Title: "Checkout Runbook", Content: "Inspect payment timeouts.",
			Score: 1,
		},
		{
			ChunkID: "new", DocumentID: "doc-new",
			Title: "Checkout Runbook", Content: "Inspect payment timeouts.",
			Score: 3,
		},
		{
			ChunkID: "other", DocumentID: "doc-other",
			Title: "Checkout Runbook", Content: "Inspect Redis saturation.",
			Score: 2,
		},
	}}
	service, _ := NewService(store, 100)
	result, err := service.HybridRetrieve(context.Background(), RetrievalRequest{
		Query: "checkout",
		TopK:  5,
	})
	if err != nil {
		t.Fatalf("HybridRetrieve() error = %v", err)
	}
	if len(result.Chunks) != 2 ||
		result.Chunks[0].DocumentID != "doc-new" ||
		result.Chunks[1].DocumentID != "doc-other" {
		t.Fatalf("result = %#v", result)
	}
	assertDuplicateCount(t, SearchResult{Metadata: result.Chunks[0].Metadata}, 1)
}
