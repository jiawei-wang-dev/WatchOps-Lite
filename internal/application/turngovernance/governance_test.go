package turngovernance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
)

type governanceStore struct {
	focus   session.SessionFocus
	saved   session.SessionFocus
	recent  []session.Message
	loadErr error
}

func (s *governanceStore) AppendMessage(
	context.Context,
	string,
	session.Message,
) error {
	return nil
}

func (s *governanceStore) GetRecentMessages(
	context.Context,
	string,
	int,
) ([]session.Message, error) {
	return s.recent, nil
}

func (s *governanceStore) GetSummary(
	context.Context,
	string,
) (session.Summary, error) {
	return session.EmptySummary(), nil
}

func (s *governanceStore) UpdateSummary(
	context.Context,
	string,
	session.Summary,
	int64,
) error {
	return nil
}

func (s *governanceStore) LoadContext(
	context.Context,
	string,
) (session.ContextSnapshot, error) {
	return session.ContextSnapshot{}, nil
}

func (s *governanceStore) ClearHistory(context.Context, string) error {
	return nil
}

func (s *governanceStore) LoadFocus(
	context.Context,
	string,
) (session.SessionFocus, error) {
	if s.loadErr != nil {
		return session.SessionFocus{}, s.loadErr
	}
	return s.focus, nil
}

func (s *governanceStore) SaveFocus(
	_ context.Context,
	_ string,
	focus session.SessionFocus,
) error {
	if focus.Version != s.focus.Version {
		return session.ErrVersionConflict
	}
	focus.Version++
	s.focus = focus
	s.saved = focus
	return nil
}

type governanceCapturingRecognizer struct {
	calls int
	input intent.RecognitionInput
}

func (r *governanceCapturingRecognizer) Recognize(
	ctx context.Context,
	input intent.RecognitionInput,
) (intent.IntentResult, error) {
	r.calls++
	r.input = input
	return intent.NewRuleBasedRecognizer().Recognize(ctx, input)
}

func TestResolverUsesBoundedFocusAndSharedSlotRules(t *testing.T) {
	messages := make([]session.Message, 8)
	for index := range messages {
		messages[index] = session.Message{
			Role:    session.RoleUser,
			Content: strings.Repeat("界", 350),
		}
	}
	store := &governanceStore{focus: session.SessionFocus{
		Version:         4,
		LastIntent:      string(intent.IntentIncidentTriage),
		KnownSlots:      map[string]string{},
		PendingQuestion: "checkout 还是 payment？",
		Candidates:      []string{"checkout", "payment"},
		TurnStatus:      session.TurnStatusClarify,
		RecentMessages:  messages,
	}}
	recognizer := &governanceCapturingRecognizer{}
	resolver := NewResolver(store, recognizer, 0.55)

	outcome, err := resolver.Resolve(context.Background(), TurnInput{
		RequestID: "req-second",
		SessionID: "ses-shared",
		UserID:    "user-shared",
		Message:   "第二个",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if recognizer.calls != 1 ||
		outcome.Decision.Decision != intent.DecisionProceed ||
		outcome.ResolvedIntent.Service != "payment" {
		t.Fatalf("calls=%d outcome=%#v", recognizer.calls, outcome)
	}
	if len(recognizer.input.RecentMessages) != 6 {
		t.Fatalf("recent messages=%d want=6", len(recognizer.input.RecentMessages))
	}
	for _, message := range recognizer.input.RecentMessages {
		if len([]rune(message.Content)) > 300 {
			t.Fatalf("message length=%d want<=300", len([]rune(message.Content)))
		}
	}
}

func TestPersistFocusBoundsRedactsAndRejectsStaleVersion(t *testing.T) {
	store := &governanceStore{focus: session.SessionFocus{
		Version:    2,
		KnownSlots: map[string]string{"service": "checkout"},
	}}
	update := FocusUpdate{
		SessionID:        "ses-cas",
		RequestID:        "req-cas",
		Message:          "查 checkout token=do-not-store " + strings.Repeat("界", 400),
		AssistantSummary: "password=do-not-store " + strings.Repeat("答", 700),
		Status:           session.TurnStatusCompleted,
		Focus:            store.focus,
		Decision: intent.IntentDecision{
			Decision:   intent.DecisionProceed,
			KnownSlots: map[string]string{"service": "checkout"},
		},
		ResolvedIntent: intent.Normalize(intent.IntentResult{
			Intent:     intent.IntentMetricsQuery,
			Confidence: 0.9,
			Service:    "checkout",
			Source:     "test",
		}),
		EvidenceIDs: []string{"evidence-1"},
		Now:         time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
	}
	if err := PersistFocus(context.Background(), store, update); err != nil {
		t.Fatalf("PersistFocus() error = %v", err)
	}
	if store.saved.Version != 3 ||
		store.saved.CurrentService != "checkout" ||
		store.saved.TurnStatus != session.TurnStatusCompleted {
		t.Fatalf("saved focus=%#v", store.saved)
	}
	for _, message := range store.saved.RecentMessages {
		if strings.Contains(message.Content, "do-not-store") ||
			len([]rune(message.Content)) > 300 {
			t.Fatalf("unsafe recent message=%q", message.Content)
		}
	}
	if strings.Contains(store.saved.Summary, "do-not-store") ||
		len([]rune(store.saved.Summary)) > 600 {
		t.Fatalf("unsafe summary=%q", store.saved.Summary)
	}
	if err := PersistFocus(context.Background(), store, update); !errors.Is(
		err,
		session.ErrVersionConflict,
	) {
		t.Fatalf("stale PersistFocus() error=%v want ErrVersionConflict", err)
	}
}

func TestResolverFocusFailureDoesNotBlockCurrentTurn(t *testing.T) {
	store := &governanceStore{loadErr: errors.New("redis unavailable")}
	outcome, err := NewResolver(
		store,
		intent.NewRuleBasedRecognizer(),
		0.55,
	).Resolve(context.Background(), TurnInput{
		RequestID: "req-degraded",
		SessionID: "ses-degraded",
		Message:   "查 checkout 错误率",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if outcome.MemoryAvailable ||
		outcome.Decision.Decision != intent.DecisionProceed ||
		outcome.ResolvedIntent.Service != "checkout" {
		t.Fatalf("outcome=%#v", outcome)
	}
}
