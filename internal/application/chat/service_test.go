package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	sessionSummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type fakeRunner struct {
	output    agenteino.AgentOutput
	err       error
	lastInput agenteino.AgentInput
}

func (f *fakeRunner) Run(
	_ context.Context,
	input agenteino.AgentInput,
) (agenteino.AgentOutput, error) {
	f.lastInput = input
	return f.output, f.err
}

type fakeSessionStore struct {
	snapshot        session.ContextSnapshot
	loadErr         error
	appendErr       error
	updateErr       error
	appended        []session.Message
	updatedSummary  session.Summary
	expectedVersion int64
}

func (f *fakeSessionStore) AppendMessage(
	_ context.Context,
	_ string,
	message session.Message,
) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	f.appended = append(f.appended, message)
	return nil
}

func (f *fakeSessionStore) GetRecentMessages(
	context.Context,
	string,
	int,
) ([]session.Message, error) {
	return f.snapshot.RecentMessages, nil
}

func (f *fakeSessionStore) GetSummary(context.Context, string) (session.Summary, error) {
	return f.snapshot.Summary, nil
}

func (f *fakeSessionStore) UpdateSummary(
	_ context.Context,
	_ string,
	summary session.Summary,
	expectedVersion int64,
) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updatedSummary = summary
	f.expectedVersion = expectedVersion
	return nil
}

func (f *fakeSessionStore) LoadContext(
	context.Context,
	string,
) (session.ContextSnapshot, error) {
	if f.loadErr != nil {
		return session.ContextSnapshot{}, f.loadErr
	}
	return f.snapshot, nil
}

func TestServicePreservesIDsAndAppendsMessages(t *testing.T) {
	runner := &fakeRunner{output: emptyAgentOutput()}
	store := &fakeSessionStore{snapshot: emptySessionSnapshot()}
	service := newTestService(runner, store, 12, 12)

	result, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.RequestID != "req-01" || result.SessionID != "ses-01" {
		t.Fatalf("result = %#v, want IDs preserved", result)
	}
	if len(store.appended) != 2 {
		t.Fatalf("appended message count = %d, want user and assistant messages", len(store.appended))
	}
	if store.appended[0].Role != session.RoleUser ||
		store.appended[1].Role != session.RoleAssistant ||
		store.appended[0].RequestID != "req-01" {
		t.Fatalf("appended messages = %#v, want role and request ID preserved", store.appended)
	}
}

func TestServiceValidatesNormalizedRequest(t *testing.T) {
	service := newTestService(&fakeRunner{}, &fakeSessionStore{}, 12, 12)
	command := validCommand()
	command.SessionID = ""

	_, err := service.Execute(context.Background(), command)

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want *ValidationError", err)
	}
	if validationErr.Field != "session_id" {
		t.Fatalf("field = %q, want session_id", validationErr.Field)
	}
}

func TestServicePassesSummaryAndRecentMessagesToAgent(t *testing.T) {
	runner := &fakeRunner{output: emptyAgentOutput()}
	store := &fakeSessionStore{snapshot: session.ContextSnapshot{
		Summary: session.Summary{
			Content: "Earlier checkout investigation",
			Version: 2,
		},
		RecentMessages: []session.Message{
			{Role: session.RoleUser, Content: "Previous question"},
		},
	}}
	service := newTestService(runner, store, 12, 12)

	_, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if runner.lastInput.SessionSummary.Version != 2 ||
		len(runner.lastInput.RecentMessages) != 1 ||
		runner.lastInput.CurrentMessage != validCommand().Message {
		t.Fatalf("AgentInput = %#v, want loaded session context", runner.lastInput)
	}
}

func TestServiceDegradesGracefullyWhenMemoryLoadFails(t *testing.T) {
	runner := &fakeRunner{output: emptyAgentOutput()}
	store := &fakeSessionStore{loadErr: errors.New("redis password=secret")}
	service := newTestService(runner, store, 12, 12)

	result, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v, want successful degraded response", err)
	}
	if result.Agent.Metadata["session_memory_available"] != false {
		t.Fatalf("metadata = %#v, want session memory unavailable", result.Agent.Metadata)
	}

	found := false
	for _, limitation := range result.Agent.Limitations {
		if limitation.Code == "SESSION_MEMORY_UNAVAILABLE" {
			found = true
			if strings.Contains(limitation.Message, "password") {
				t.Fatalf("limitation exposes raw Redis error: %q", limitation.Message)
			}
		}
	}
	if !found {
		t.Fatalf("limitations = %#v, want SESSION_MEMORY_UNAVAILABLE", result.Agent.Limitations)
	}
	if len(store.appended) != 0 {
		t.Fatalf("appended messages = %#v, want writes skipped after load failure", store.appended)
	}
}

func TestServiceSummarizesMessagesLeavingRecentWindow(t *testing.T) {
	recent := make([]session.Message, 0, 12)
	for index := 0; index < 12; index++ {
		recent = append(recent, session.Message{
			Role:      session.RoleUser,
			Content:   fmt.Sprintf("older message %d about checkout", index),
			RequestID: fmt.Sprintf("req-%02d", index),
		})
	}

	runner := &fakeRunner{output: emptyAgentOutput()}
	store := &fakeSessionStore{snapshot: session.ContextSnapshot{
		Summary: session.Summary{
			Version:           3,
			ConfirmedFacts:    []string{},
			OpenQuestions:     []string{},
			AttemptedActions:  []string{},
			ImportantEntities: []string{},
		},
		RecentMessages: recent,
	}}
	service := newTestService(runner, store, 12, 12)

	_, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if store.expectedVersion != 3 {
		t.Fatalf("expected summary version = %d, want 3", store.expectedVersion)
	}
	if !strings.Contains(store.updatedSummary.Content, "older message 0") ||
		!strings.Contains(store.updatedSummary.Content, "older message 1") {
		t.Fatalf("summary = %#v, want two messages leaving the window", store.updatedSummary)
	}
	if strings.Contains(store.updatedSummary.Content, "older message 2") {
		t.Fatalf("summary = %#v, should not summarize retained messages", store.updatedSummary)
	}
}

func TestServiceDoesNotTrimHistoryWhenSummaryUpdateFails(t *testing.T) {
	recent := make([]session.Message, 12)
	for index := range recent {
		recent[index] = session.Message{
			Role:    session.RoleUser,
			Content: fmt.Sprintf("older message %d", index),
		}
	}

	runner := &fakeRunner{output: emptyAgentOutput()}
	store := &fakeSessionStore{
		snapshot: session.ContextSnapshot{
			Summary:        session.EmptySummary(),
			RecentMessages: recent,
		},
		updateErr: errors.New("summary write failed"),
	}
	service := newTestService(runner, store, 12, 12)

	result, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v, want successful degraded response", err)
	}
	if len(store.appended) != 0 {
		t.Fatalf("appended messages = %#v, want no trim-producing append", store.appended)
	}
	for _, limitation := range result.Agent.Limitations {
		if limitation.Code == "SESSION_MEMORY_UNAVAILABLE" {
			return
		}
	}
	t.Fatalf("limitations = %#v, want SESSION_MEMORY_UNAVAILABLE", result.Agent.Limitations)
}

func newTestService(
	runner agenteino.AgentRunner,
	store session.Store,
	window int,
	threshold int,
) *Service {
	return NewService(
		runner,
		store,
		sessionSummary.NewDeterministic(),
		ServiceConfig{
			RecentWindowSize: window,
			SummaryThreshold: threshold,
		},
	)
}

func validCommand() Command {
	return Command{
		RequestID: "req-01",
		SessionID: "ses-01",
		Message:   "show checkout error rate",
		TimeContext: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
	}
}

func emptyAgentOutput() agenteino.AgentOutput {
	return agenteino.AgentOutput{
		Conclusions:     []agenteino.Conclusion{},
		Evidence:        []common.EvidenceItem{},
		Inferences:      []agenteino.Inference{},
		Recommendations: []agenteino.Recommendation{},
		Limitations:     []agenteino.Limitation{},
		ToolRuns:        []agenteino.ToolRun{},
		Metadata:        map[string]any{},
	}
}

func emptySessionSnapshot() session.ContextSnapshot {
	return session.ContextSnapshot{
		Summary:        session.EmptySummary(),
		RecentMessages: []session.Message{},
	}
}
