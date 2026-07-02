package longterm

import (
	"context"
	"errors"
	"testing"
	"time"
)

type storeStub struct {
	saved     Memory
	results   []Memory
	found     Memory
	query     SearchQuery
	saveErr   error
	searchErr error
	getErr    error
}

func (s *storeStub) Save(_ context.Context, memory Memory) error {
	s.saved = memory
	return s.saveErr
}

func (s *storeStub) Search(
	_ context.Context,
	query SearchQuery,
) ([]Memory, error) {
	s.query = query
	return s.results, s.searchErr
}

func (s *storeStub) Get(context.Context, string) (Memory, error) {
	return s.found, s.getErr
}

func TestServiceNormalizesAndSavesConfirmedMemory(t *testing.T) {
	store := &storeStub{}
	service, err := NewService(store, 3)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.newID = func() (string, error) { return "mem-fixed", nil }
	service.now = func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	}

	err = service.Save(context.Background(), Memory{
		SourceType:  " FEEDBACK_UP ",
		SourceID:    " fb-1 ",
		Service:     " checkout ",
		Title:       " Confirmed timeout ",
		Summary:     " Payment latency increased. ",
		EvidenceIDs: []string{"metric-1", "metric-1", "log-1"},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if store.saved.ID != "mem-fixed" ||
		store.saved.SourceType != SourceFeedbackUp ||
		len(store.saved.EvidenceIDs) != 2 ||
		store.saved.CreatedAt.IsZero() ||
		store.saved.UpdatedAt.IsZero() {
		t.Fatalf("saved memory = %#v", store.saved)
	}
}

func TestServiceUsesBoundedDefaultSearchLimit(t *testing.T) {
	store := &storeStub{results: []Memory{}}
	service, _ := NewService(store, 3)

	results, err := service.Search(context.Background(), SearchQuery{
		Query: " checkout timeout ",
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if results == nil || store.query.Limit != 3 ||
		store.query.Query != "checkout timeout" {
		t.Fatalf("results=%#v query=%#v", results, store.query)
	}
}

func TestServiceRejectsUnconfirmedNonManualMemory(t *testing.T) {
	service, _ := NewService(&storeStub{}, 3)
	err := service.Save(context.Background(), Memory{
		SourceType: SourceFeedbackUp,
		SourceID:   "fb-1",
		Title:      "Unsupported",
		Summary:    "No evidence is attached.",
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Save() error = %v, want ErrInvalidArgument", err)
	}
}
