package feedback

import (
	"context"
	"errors"
	"testing"
	"time"
)

type storeStub struct {
	created Feedback
	stored  Feedback
	err     error
}

func (s *storeStub) Create(_ context.Context, value Feedback) error {
	s.created = value
	return s.err
}

func (s *storeStub) Get(context.Context, string) (Feedback, error) {
	return s.stored, s.err
}

func TestServiceCreatesValidatedFeedback(t *testing.T) {
	store := &storeStub{}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.newID = func() (string, error) { return "fb_fixed", nil }
	service.now = func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

	result, err := service.Create(context.Background(), Feedback{
		RequestID:  " req-123 ",
		SessionID:  " ses_01 ",
		Rating:     "DOWN",
		ReasonTags: []string{"missing_evidence"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.FeedbackID != "fb_fixed" || store.created.Rating != RatingDown {
		t.Fatalf("result=%#v feedback=%#v", result, store.created)
	}
	if store.created.AnswerSnapshot == nil || store.created.ToolRuns == nil {
		t.Fatalf("optional JSON fields were not normalized: %#v", store.created)
	}
}

func TestServiceRejectsInvalidFeedback(t *testing.T) {
	service, _ := NewService(&storeStub{})
	tests := []Feedback{
		{SessionID: "ses", Rating: RatingUp},
		{RequestID: "req", Rating: RatingUp},
		{RequestID: "req", SessionID: "ses", Rating: "neutral"},
		{RequestID: "req", SessionID: "ses", Rating: RatingDown, ReasonTags: []string{""}},
	}
	for _, input := range tests {
		if _, err := service.Create(context.Background(), input); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("Create(%#v) error = %v, want ErrInvalidArgument", input, err)
		}
	}
}

func TestUnavailableStoreReturnsUnavailable(t *testing.T) {
	service, _ := NewService(UnavailableStore{})
	if _, err := service.Create(context.Background(), Feedback{
		RequestID: "req", SessionID: "ses", Rating: RatingUp,
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Create() error = %v, want ErrUnavailable", err)
	}
	if _, err := service.Get(context.Background(), "fb_1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Get() error = %v, want ErrUnavailable", err)
	}
}
