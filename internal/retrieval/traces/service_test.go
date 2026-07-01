package traces

import (
	"context"
	"testing"
	"time"
)

type storeStub struct {
	spans []Span
	query Query
}

func (s *storeStub) Search(_ context.Context, query Query) ([]Span, error) {
	s.query = query
	return s.spans, nil
}

func TestServiceFiltersAndRanksTraceSpans(t *testing.T) {
	store := &storeStub{spans: []Span{
		{SpanID: "slow", Service: "watchops-lite", Operation: "agent.run", DurationMS: 300},
		{SpanID: "error", Service: "watchops-lite", Operation: "agent.run", DurationMS: 20, Error: true},
		{SpanID: "other", Service: "other", Operation: "agent.run", DurationMS: 500},
	}}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	now := time.Now().UTC()
	spans, err := service.Search(context.Background(), Query{
		Service:   "watchops-lite",
		Operation: "agent",
		From:      now.Add(-time.Minute),
		To:        now,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(spans) != 2 || spans[0].SpanID != "error" || spans[1].SpanID != "slow" {
		t.Fatalf("spans = %#v", spans)
	}
}
