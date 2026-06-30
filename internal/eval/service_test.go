package eval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/feedback"
)

type storeStub struct {
	created   Case
	listQuery ListQuery
	cases     []Case
	err       error
}

func (s *storeStub) Create(_ context.Context, value Case) error {
	s.created = value
	return s.err
}

func (s *storeStub) List(_ context.Context, query ListQuery) ([]Case, error) {
	s.listQuery = query
	return s.cases, s.err
}

type feedbackReaderStub struct {
	value feedback.Feedback
	err   error
}

func (s feedbackReaderStub) Get(context.Context, string) (feedback.Feedback, error) {
	return s.value, s.err
}

func TestServiceCreatesCaseMatchingFeedbackRating(t *testing.T) {
	store := &storeStub{}
	service, err := NewService(store, feedbackReaderStub{
		value: feedback.Feedback{ID: "fb_1", Rating: feedback.RatingDown},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.newID = func() (string, error) { return "eval_fixed", nil }
	service.now = func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

	result, err := service.Create(context.Background(), Case{
		FeedbackID:       "fb_1",
		CaseType:         CaseTypeBad,
		InputMessage:     "Why did checkout fail?",
		ExpectedBehavior: "Cite evidence and report missing trace data.",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.CaseID != "eval_fixed" || store.created.CaseType != CaseTypeBad {
		t.Fatalf("result=%#v case=%#v", result, store.created)
	}
}

func TestServiceRejectsCaseThatConflictsWithFeedbackRating(t *testing.T) {
	service, _ := NewService(&storeStub{}, feedbackReaderStub{
		value: feedback.Feedback{Rating: feedback.RatingUp},
	})
	_, err := service.Create(context.Background(), Case{
		FeedbackID:       "fb_1",
		CaseType:         CaseTypeBad,
		InputMessage:     "Question",
		ExpectedBehavior: "Expected behavior",
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Create() error = %v, want ErrInvalidArgument", err)
	}
}

func TestServiceValidatesAndDefaultsListQuery(t *testing.T) {
	store := &storeStub{}
	service, _ := NewService(store, feedbackReaderStub{})

	if _, err := service.List(context.Background(), ListQuery{CaseType: "unknown"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("List() error = %v, want ErrInvalidArgument", err)
	}
	if _, err := service.List(context.Background(), ListQuery{CaseType: CaseTypeGood}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if store.listQuery.Limit != defaultListLimit {
		t.Fatalf("list limit = %d, want %d", store.listQuery.Limit, defaultListLimit)
	}
}

func TestUnavailableStoreReturnsUnavailable(t *testing.T) {
	service, _ := NewService(UnavailableStore{}, feedbackReaderStub{})
	if _, err := service.List(context.Background(), ListQuery{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("List() error = %v, want ErrUnavailable", err)
	}
}
