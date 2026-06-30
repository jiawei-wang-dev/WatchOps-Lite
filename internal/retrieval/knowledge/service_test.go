package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"
)

type storeStub struct {
	indexed []Chunk
	results []SearchResult
	info    DocumentInfo
	err     error
}

func (s *storeStub) EnsureIndex(context.Context) error { return s.err }
func (s *storeStub) IndexChunks(_ context.Context, chunks []Chunk) error {
	s.indexed = chunks
	return s.err
}
func (s *storeStub) Search(context.Context, SearchQuery) ([]SearchResult, error) {
	return s.results, s.err
}
func (s *storeStub) GetDocument(context.Context, string) (DocumentInfo, error) {
	return s.info, s.err
}

func TestServiceIngestsDeterministicChunks(t *testing.T) {
	store := &storeStub{}
	service, err := NewService(store, 20)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.newID = func() (string, error) { return "doc_fixed", nil }
	service.now = func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

	result, err := service.Ingest(context.Background(), Document{
		Title:   "Checkout runbook",
		Source:  "runbook",
		Content: "Inspect latency.\n\nInspect retries and saturation.",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if result.DocumentID != "doc_fixed" || result.ChunkCount != len(store.indexed) {
		t.Fatalf("result = %#v, indexed=%d", result, len(store.indexed))
	}
	if store.indexed[0].ID != "doc_fixed_chunk_0000" {
		t.Fatalf("chunk ID = %q", store.indexed[0].ID)
	}
}

func TestServiceValidatesSearchAndPropagatesUnavailable(t *testing.T) {
	service, _ := NewService(&storeStub{err: ErrUnavailable}, 100)

	if _, err := service.Search(context.Background(), SearchQuery{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty search error = %v, want ErrInvalidArgument", err)
	}
	if _, err := service.Search(context.Background(), SearchQuery{Query: "timeouts"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("search error = %v, want ErrUnavailable", err)
	}
}
