package chat

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	sessionSummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
)

func TestGetHistoryAppliesDefaultAndMaximumLimits(t *testing.T) {
	messages := make([]session.Message, 120)
	for index := range messages {
		messages[index] = session.Message{
			Role:    session.RoleUser,
			Content: fmt.Sprintf("message-%03d", index),
		}
	}
	tests := []struct {
		name      string
		requested int
		wantLimit int
		wantFirst string
	}{
		{
			name:      "default",
			requested: 0,
			wantLimit: defaultHistoryLimit,
			wantFirst: "message-100",
		},
		{
			name:      "maximum",
			requested: 500,
			wantLimit: maxHistoryLimit,
			wantFirst: "message-020",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeSessionStore{snapshot: session.ContextSnapshot{
				Summary:        session.EmptySummary(),
				RecentMessages: messages,
			}}
			service := newTestService(
				&fakeRunner{output: emptyAgentOutput()},
				store,
				12,
				12,
			)

			result, err := service.GetHistory(
				context.Background(),
				HistoryQuery{SessionID: " session-1 ", Limit: test.requested},
			)
			if err != nil {
				t.Fatalf("GetHistory() error = %v", err)
			}
			if result.SessionID != "session-1" ||
				result.Limit != test.wantLimit ||
				len(result.Messages) != test.wantLimit ||
				result.Messages[0].Content != test.wantFirst {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestGetHistoryRejectsMissingSessionID(t *testing.T) {
	service := newTestService(
		&fakeRunner{output: emptyAgentOutput()},
		&fakeSessionStore{},
		12,
		12,
	)

	_, err := service.GetHistory(context.Background(), HistoryQuery{})

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "session_id" {
		t.Fatalf("GetHistory() error = %v, want session_id validation", err)
	}
}

func TestClearHistoryUsesOnlySessionStore(t *testing.T) {
	store := &fakeSessionStore{snapshot: session.ContextSnapshot{
		Summary: session.Summary{Content: "summary", Version: 2},
		RecentMessages: []session.Message{{
			Role:    session.RoleUser,
			Content: "message",
		}},
	}}
	service := newTestService(
		&fakeRunner{output: emptyAgentOutput()},
		store,
		12,
		12,
	)

	if err := service.ClearHistory(context.Background(), "session-1"); err != nil {
		t.Fatalf("ClearHistory() error = %v", err)
	}
	if !store.cleared ||
		len(store.snapshot.RecentMessages) != 0 ||
		store.snapshot.Summary.Version != 0 {
		t.Fatalf("store = %#v, want cleared session state", store)
	}
}

func TestClearHistoryDoesNotDeleteLongTermMemory(t *testing.T) {
	sessionStore := &fakeSessionStore{}
	longTermStore := &fakeLongTermMemoryStore{memories: []longterm.Memory{{
		ID:      "memory-1",
		Summary: "confirmed checkout incident",
	}}}
	service := NewService(
		&fakeRunner{output: emptyAgentOutput()},
		sessionStore,
		sessionSummary.NewDeterministic(),
		ServiceConfig{
			RecentWindowSize:   12,
			SummaryThreshold:   12,
			LongTermMemory:     longTermStore,
			LongTermMemoryTopK: 3,
		},
	)

	if err := service.ClearHistory(context.Background(), "session-1"); err != nil {
		t.Fatalf("ClearHistory() error = %v", err)
	}
	memories, err := longTermStore.Search(
		context.Background(),
		longterm.SearchQuery{Query: "checkout", Limit: 3},
	)
	if err != nil || len(memories) != 1 || memories[0].ID != "memory-1" {
		t.Fatalf("long-term memories = %#v, error = %v", memories, err)
	}
}

func TestHistoryReturnsStructuredUnavailableError(t *testing.T) {
	store := &fakeSessionStore{clearErr: errors.New("redis unavailable")}
	service := newTestService(
		&fakeRunner{output: emptyAgentOutput()},
		store,
		12,
		12,
	)

	err := service.ClearHistory(context.Background(), "session-1")

	if !errors.Is(err, ErrSessionMemoryUnavailable) {
		t.Fatalf("ClearHistory() error = %v, want ErrSessionMemoryUnavailable", err)
	}
}
