package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	sessionSummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
)

type capturingIntentRecognizer struct {
	input  intent.RecognitionInput
	result intent.IntentResult
}

type failingIntentRecognizer struct{}

func (failingIntentRecognizer) Recognize(
	context.Context,
	intent.RecognitionInput,
) (intent.IntentResult, error) {
	return intent.IntentResult{}, errors.New("provider secret=do-not-expose")
}

func (r *capturingIntentRecognizer) Recognize(
	_ context.Context,
	input intent.RecognitionInput,
) (intent.IntentResult, error) {
	r.input = input
	if r.result.Intent == "" {
		return intent.NewRuleBasedRecognizer().Recognize(context.Background(), input)
	}
	return r.result, nil
}

func TestContextAwareIntentReceivesBoundedFocus(t *testing.T) {
	messages := make([]session.Message, 8)
	for index := range messages {
		messages[index] = session.Message{
			Role:      session.RoleUser,
			Content:   strings.Repeat("界", 350),
			CreatedAt: time.Date(2026, 7, 30, 0, index, 0, 0, time.UTC),
		}
	}
	store := &fakeSessionStore{focus: session.SessionFocus{
		LastIntent:      intent.IntentMetricsQuery.String(),
		KnownSlots:      map[string]string{"service": "checkout"},
		PendingQuestion: "需要排查 checkout 还是 payment？",
		TurnStatus:      session.TurnStatusClarify,
		RecentMessages:  messages,
	}}
	recognizer := &capturingIntentRecognizer{result: intent.IntentResult{
		Intent: intent.IntentMetricsQuery, Confidence: 0.9,
		Service: "checkout", Source: "test",
	}}
	service := NewService(
		&fakeRunner{output: emptyAgentOutput()},
		store,
		sessionSummary.NewDeterministic(),
		ServiceConfig{IntentRecognizer: recognizer},
	)
	if _, err := service.Execute(context.Background(), validCommand()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(recognizer.input.RecentMessages) != 6 {
		t.Fatalf("recent messages = %d, want 6", len(recognizer.input.RecentMessages))
	}
	for _, message := range recognizer.input.RecentMessages {
		if len([]rune(message.Content)) > 300 {
			t.Fatalf("message length = %d, want <= 300", len([]rune(message.Content)))
		}
	}
	if recognizer.input.Focus.KnownSlots["service"] != "checkout" ||
		recognizer.input.Focus.PendingQuestion == "" ||
		!recognizer.input.Focus.Available {
		t.Fatalf("focus input = %#v", recognizer.input.Focus)
	}
}

func TestBoundSessionFocusDropsSystemMessagesAndClonesMutableState(t *testing.T) {
	known := map[string]string{"service": "checkout"}
	focus := boundSessionFocus(session.SessionFocus{
		KnownSlots: known,
		RecentMessages: []session.Message{
			{Role: session.RoleSystem, Content: "private system prompt"},
			{Role: session.RoleUser, Content: "checkout"},
		},
	})
	known["service"] = "payment"
	if focus.KnownSlots["service"] != "checkout" {
		t.Fatalf("focus map aliases caller map: %#v", focus.KnownSlots)
	}
	if len(focus.RecentMessages) != 1 ||
		focus.RecentMessages[0].Role != session.RoleUser {
		t.Fatalf("recent messages = %#v, system content must be excluded", focus.RecentMessages)
	}
}

func TestRuleIntentResolvesSecondCandidate(t *testing.T) {
	result, err := intent.NewRuleBasedRecognizer().Recognize(
		context.Background(),
		intent.RecognitionInput{
			Message: "第二个",
			Focus: intent.FocusView{
				Available:  true,
				LastIntent: string(intent.IntentIncidentTriage),
				Candidates: []string{"checkout", "payment"},
				KnownSlots: map[string]string{},
			},
		},
	)
	if err != nil || result.Service != "payment" ||
		result.Intent != intent.IntentIncidentTriage {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestRuleIntentKeepsServiceAndOverridesTime(t *testing.T) {
	result, err := intent.NewRuleBasedRecognizer().Recognize(
		context.Background(),
		intent.RecognitionInput{
			Message: "换成昨天",
			Focus: intent.FocusView{
				Available:  true,
				LastIntent: string(intent.IntentMetricsQuery),
				KnownSlots: map[string]string{"service": "checkout"},
			},
		},
	)
	if err != nil || result.Service != "checkout" ||
		result.TimeRange == nil || result.TimeRange.Relative != "yesterday" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestCurrentRunbookIntentOverridesIncidentFocus(t *testing.T) {
	result, err := intent.NewRuleBasedRecognizer().Recognize(
		context.Background(),
		intent.RecognitionInput{
			Message: "另外帮我找一下 runbook",
			Focus: intent.FocusView{
				Available:  true,
				LastIntent: string(intent.IntentIncidentTriage),
				KnownSlots: map[string]string{"service": "checkout"},
			},
		},
	)
	if err != nil || result.Intent != intent.IntentKnowledgeQuery {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestClarificationSkipsRunnerAndPersistsTurnState(t *testing.T) {
	runner := &fakeRunner{output: emptyAgentOutput()}
	store := &fakeSessionStore{}
	service := newTestService(runner, store, 12, 12)
	command := validCommand()
	command.Message = "查错误率"
	result, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runner.calls != 0 || len(result.Agent.ToolRuns) != 0 ||
		len(result.Agent.Evidence) != 0 {
		t.Fatalf("runner calls=%d output=%#v", runner.calls, result.Agent)
	}
	if result.Agent.Metadata["status"] != "clarification_required" ||
		store.savedFocus.TurnStatus != session.TurnStatusClarify ||
		store.savedFocus.PendingQuestion == "" {
		t.Fatalf("metadata=%#v focus=%#v", result.Agent.Metadata, store.savedFocus)
	}
}

func TestClarificationPersistenceFailureDoesNotFailChat(t *testing.T) {
	store := &fakeSessionStore{focusSaveErr: errors.New("redis unavailable")}
	service := newTestService(&fakeRunner{output: emptyAgentOutput()}, store, 12, 12)
	command := validCommand()
	command.Message = "分析 trace"
	result, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Agent.Metadata["persistence_failed"] != true {
		t.Fatalf("metadata=%#v", result.Agent.Metadata)
	}
}

func TestNormalFocusPersistenceFailureDoesNotReplaceAnswer(t *testing.T) {
	output := emptyAgentOutput()
	output.Conclusions = []agenteino.Conclusion{{Text: "checkout diagnosis remains available"}}
	store := &fakeSessionStore{focusSaveErr: errors.New("redis unavailable")}
	service := newTestService(&fakeRunner{output: output}, store, 12, 12)
	result, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Agent.Conclusions) != 1 ||
		result.Agent.Conclusions[0].Text != "checkout diagnosis remains available" {
		t.Fatalf("answer was replaced after Focus persistence failure: %#v", result.Agent)
	}
	if result.Agent.Metadata["session_focus_persistence_failed"] != true {
		t.Fatalf("metadata=%#v", result.Agent.Metadata)
	}
}

func TestSessionFocusLoadFailureStillRecognizesIntent(t *testing.T) {
	runner := &fakeRunner{output: emptyAgentOutput()}
	store := &fakeSessionStore{focusLoadErr: errors.New("redis unavailable")}
	service := newTestService(runner, store, 12, 12)
	result, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runner.calls != 1 ||
		result.Agent.Metadata["session_focus_available"] != false {
		t.Fatalf("calls=%d metadata=%#v", runner.calls, result.Agent.Metadata)
	}
}

func TestIntentRecognizerFailureUsesSafeFallbackWithoutLeakingError(t *testing.T) {
	runner := &fakeRunner{output: emptyAgentOutput()}
	service := NewService(
		runner,
		&fakeSessionStore{},
		sessionSummary.NewDeterministic(),
		ServiceConfig{IntentRecognizer: failingIntentRecognizer{}},
	)
	result, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	intentValue, ok := result.Agent.Metadata["intent"].(intent.IntentResult)
	if !ok || intentValue.Source != "fallback" {
		t.Fatalf("intent metadata = %#v", result.Agent.Metadata["intent"])
	}
	for _, limitation := range intentValue.Limitations {
		if strings.Contains(limitation.Message, "do-not-expose") {
			t.Fatalf("limitation leaks provider error: %#v", limitation)
		}
	}
}

func TestClarificationNextTurnRecoversIntentAndProceeds(t *testing.T) {
	runner := &fakeRunner{output: emptyAgentOutput()}
	store := &fakeSessionStore{focus: session.SessionFocus{
		LastIntent:      string(intent.IntentMetricsQuery),
		KnownSlots:      map[string]string{},
		MissingSlots:    []string{"service"},
		PendingQuestion: "需要排查哪个服务？",
		TurnStatus:      session.TurnStatusClarify,
	}}
	service := newTestService(runner, store, 12, 12)
	command := validCommand()
	command.Message = "checkout"
	result, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runner.calls != 1 || result.Agent.Metadata["status"] == "clarification_required" {
		t.Fatalf("calls=%d metadata=%#v", runner.calls, result.Agent.Metadata)
	}
	if store.savedFocus.KnownSlots["service"] != "checkout" ||
		store.savedFocus.TurnStatus != session.TurnStatusCompleted {
		t.Fatalf("saved focus=%#v", store.savedFocus)
	}
}

func TestClarificationStreamDoesNotEmitToolOrEvidenceEvents(t *testing.T) {
	runner := &fakeRunner{output: emptyAgentOutput()}
	retriever := &recordingKnowledgeRetriever{}
	service := NewService(
		runner,
		&fakeSessionStore{},
		sessionSummary.NewDeterministic(),
		ServiceConfig{KnowledgeRetriever: retriever},
	)
	command := validCommand()
	command.Message = "查错误率"
	events := []string{}
	startedNodes := []string{}
	result, err := service.Stream(context.Background(), command, func(event StreamEvent) {
		events = append(events, event.Type)
		if event.Type == "graph_node_started" {
			if node, ok := event.Data["node"].(string); ok {
				startedNodes = append(startedNodes, node)
			}
		}
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if runner.calls != 0 || len(retriever.requests) != 0 {
		t.Fatalf("runner calls=%d retrieval calls=%d", runner.calls, len(retriever.requests))
	}
	for _, forbidden := range []string{
		nodeProceedIntent,
		nodeLoadSessionContext,
		nodeLoadLongTermMemory,
		nodeLoadUserProfile,
		nodePrepareSkills,
		nodePreRetrieveKnowledge,
		nodeMergeContext,
		nodeRenderPromptTemplate,
		nodeRunReActAgent,
		nodeCollectToolEvidence,
		nodePersistSessionMemory,
		nodePersistSessionFocus,
	} {
		for _, node := range startedNodes {
			if node == forbidden {
				t.Fatalf("started nodes=%v contain forbidden %q", startedNodes, forbidden)
			}
		}
	}
	if result.Agent.Metadata["status"] != "clarification_required" {
		t.Fatalf("metadata=%#v", result.Agent.Metadata)
	}
	for _, forbidden := range []string{
		"tool_started", "tool_completed", "evidence_collected",
		"pre_rag_started", "pre_rag_completed",
	} {
		for _, event := range events {
			if event == forbidden {
				t.Fatalf("events=%v contain forbidden %q", events, forbidden)
			}
		}
	}
	for _, required := range []string{
		"intent_recognized", "slot_validation_completed", "clarification_required",
	} {
		found := false
		for _, event := range events {
			found = found || event == required
		}
		if !found {
			t.Fatalf("events=%v missing %q", events, required)
		}
	}
}
