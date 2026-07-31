package multiagent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/compose"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type serviceSessionStore struct {
	snapshot session.ContextSnapshot
	loadErr  error
	appended []session.Message
	focus    session.SessionFocus
	saved    session.SessionFocus
	focusErr error
	saveErr  error
}

func (s *serviceSessionStore) AppendMessage(
	_ context.Context,
	_ string,
	message session.Message,
) error {
	s.appended = append(s.appended, message)
	return nil
}

func (s *serviceSessionStore) GetRecentMessages(
	context.Context,
	string,
	int,
) ([]session.Message, error) {
	return s.snapshot.RecentMessages, nil
}

func (s *serviceSessionStore) GetSummary(
	context.Context,
	string,
) (session.Summary, error) {
	return s.snapshot.Summary, nil
}

func (s *serviceSessionStore) UpdateSummary(
	context.Context,
	string,
	session.Summary,
	int64,
) error {
	return nil
}

func (s *serviceSessionStore) LoadContext(
	context.Context,
	string,
) (session.ContextSnapshot, error) {
	if s.loadErr != nil {
		return session.ContextSnapshot{}, s.loadErr
	}
	return s.snapshot, nil
}

func (s *serviceSessionStore) ClearHistory(context.Context, string) error {
	return nil
}

func (s *serviceSessionStore) LoadFocus(
	context.Context,
	string,
) (session.SessionFocus, error) {
	if s.focusErr != nil {
		return session.SessionFocus{}, s.focusErr
	}
	if s.focus.KnownSlots == nil {
		return session.EmptyFocus(), nil
	}
	return s.focus, nil
}

func (s *serviceSessionStore) SaveFocus(
	_ context.Context,
	_ string,
	focus session.SessionFocus,
) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	if focus.Version != s.focus.Version {
		return session.ErrVersionConflict
	}
	focus.Version++
	s.focus = focus
	s.saved = focus
	return nil
}

type countingRecognizer struct {
	calls int
	input intent.RecognitionInput
}

func (r *countingRecognizer) Recognize(
	ctx context.Context,
	input intent.RecognitionInput,
) (intent.IntentResult, error) {
	r.calls++
	r.input = input
	return intent.NewRuleBasedRecognizer().Recognize(ctx, input)
}

type recordingGraphRunner struct {
	calls int
	input Input
}

func (r *recordingGraphRunner) Invoke(
	_ context.Context,
	input Input,
	_ ...compose.Option,
) (MultiAgentResult, error) {
	r.calls++
	r.input = input
	return MultiAgentResult{
		Steps:    []AgentStep{},
		Evidence: []common.EvidenceItem{},
		ToolRuns: []agenteino.ToolRun{},
		FinalAnswer: agenteino.AgentOutput{
			Conclusions: []agenteino.Conclusion{{Text: "completed"}},
			Evidence:    []common.EvidenceItem{},
			ToolRuns:    []agenteino.ToolRun{},
			Metadata:    map[string]any{},
		},
		Metadata: map[string]any{},
	}, nil
}

func TestServiceLoadsAndPersistsSessionMemory(t *testing.T) {
	store := &serviceSessionStore{snapshot: session.ContextSnapshot{
		Summary: session.Summary{Content: "Previous checkout context", Version: 3},
		RecentMessages: []session.Message{{
			Role:    session.RoleUser,
			Content: "Previous question",
		}},
	}}
	service := NewService(testOrchestrator(t)).WithSessionMemory(store)
	service.now = func() time.Time {
		return time.Date(2026, 7, 9, 1, 2, 3, 0, time.UTC)
	}

	result, err := service.Execute(context.Background(), Command{
		RequestID: "req-memory",
		SessionID: "ses-memory",
		Message:   "Why is checkout failing?",
		TimeContext: common.TimeRange{
			From: "2026-07-09T00:00:00Z",
			To:   "2026-07-09T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Output.Metadata["session_memory_available"] != true ||
		result.Output.Metadata["session_context_loaded"] != true ||
		result.Output.Metadata["recent_message_count"] != 1 ||
		result.Output.Metadata["summary_version"] != int64(3) {
		t.Fatalf("metadata = %#v", result.Output.Metadata)
	}
	if len(store.appended) != 2 ||
		store.appended[0].Role != session.RoleUser ||
		store.appended[1].Role != session.RoleAssistant {
		t.Fatalf("appended = %#v", store.appended)
	}
}

func TestServiceDegradesWhenSessionMemoryUnavailable(t *testing.T) {
	store := &serviceSessionStore{loadErr: errors.New("redis unavailable")}
	service := NewService(testOrchestrator(t)).WithSessionMemory(store)

	result, err := service.Execute(context.Background(), Command{
		RequestID: "req-memory-down",
		SessionID: "ses-memory-down",
		Message:   "Why is checkout failing?",
		TimeContext: common.TimeRange{
			From: "2026-07-09T00:00:00Z",
			To:   "2026-07-09T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Output.Metadata["session_memory_available"] != false ||
		result.Output.Metadata["session_context_loaded"] != false {
		t.Fatalf("metadata = %#v", result.Output.Metadata)
	}
	if len(store.appended) != 0 {
		t.Fatalf("appended = %#v, want none", store.appended)
	}
}

func TestServiceGovernanceClarifiesBeforeOrchestrator(t *testing.T) {
	store := &serviceSessionStore{}
	recognizer := &countingRecognizer{}
	graph := &recordingGraphRunner{}
	orchestrator := testOrchestrator(t)
	orchestrator.graph = graph
	service := NewService(orchestrator).
		WithIntentRecognizer(recognizer).
		WithSessionMemory(store)

	result, err := service.Execute(context.Background(), Command{
		RequestID:   "req-clarify",
		SessionID:   "ses-clarify",
		Message:     "帮我查一下错误率",
		TimeContext: governanceTestTime(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if recognizer.calls != 1 || graph.calls != 0 {
		t.Fatalf("recognizer calls=%d graph calls=%d", recognizer.calls, graph.calls)
	}
	if result.Output.Metadata["decision"] != "clarify" ||
		result.Output.Metadata["error_code"] != "CLARIFICATION_REQUIRED" ||
		len(result.Output.Steps) != 0 ||
		len(result.Output.Evidence) != 0 ||
		len(result.Output.ToolRuns) != 0 ||
		len(result.Output.FinalAnswer.Recommendations) != 0 {
		t.Fatalf("result = %#v", result.Output)
	}
	if store.saved.TurnStatus != session.TurnStatusClarify ||
		store.saved.PendingQuestion == "" ||
		len(store.saved.MissingSlots) != 1 ||
		store.saved.MissingSlots[0] != "service" ||
		store.saved.Version != 1 {
		t.Fatalf("saved focus = %#v", store.saved)
	}
}

func TestServiceGovernanceProceedsWithValidatedIntentOnce(t *testing.T) {
	store := &serviceSessionStore{}
	recognizer := &countingRecognizer{}
	graph := &recordingGraphRunner{}
	orchestrator := testOrchestrator(t)
	orchestrator.graph = graph
	service := NewService(orchestrator).
		WithIntentRecognizer(recognizer).
		WithSessionMemory(store)
	service.now = func() time.Time {
		return time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	}

	result, err := service.Execute(context.Background(), Command{
		RequestID:   "req-proceed",
		SessionID:   "ses-proceed",
		Message:     "查 payment 最近十分钟的错误率",
		TimeContext: governanceTestTime(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if recognizer.calls != 1 || graph.calls != 1 {
		t.Fatalf("recognizer calls=%d graph calls=%d", recognizer.calls, graph.calls)
	}
	if graph.input.Intent.Intent != intent.IntentMetricsQuery ||
		graph.input.Intent.Service != "payment" ||
		graph.input.AgentPlan.Intent.Intent != intent.IntentMetricsQuery ||
		graph.input.AgentPlan.Intent.Service != "payment" ||
		graph.input.TimeContext.From != "2026-07-31T07:50:00Z" ||
		graph.input.TimeContext.To != "2026-07-31T08:00:00Z" {
		t.Fatalf("orchestrator input = %#v", graph.input)
	}
	if result.Output.Metadata["turn_governance_decision"] != "proceed" ||
		store.saved.CurrentService != "payment" ||
		store.saved.CurrentTimeRange != "last_10_minutes" ||
		store.saved.TurnStatus != session.TurnStatusCompleted {
		t.Fatalf("result=%#v focus=%#v", result.Output.Metadata, store.saved)
	}
}

func TestServiceGovernanceRecoversAndOverridesFocus(t *testing.T) {
	tests := []struct {
		name         string
		focus        session.SessionFocus
		message      string
		wantIntent   intent.IntentType
		wantService  string
		wantTime     string
		wantDecision string
		wantReason   string
	}{
		{
			name: "clarification recovery",
			focus: session.SessionFocus{
				LastIntent:      string(intent.IntentMetricsQuery),
				KnownSlots:      map[string]string{},
				MissingSlots:    []string{"service"},
				PendingQuestion: "需要排查哪个服务？",
				TurnStatus:      session.TurnStatusClarify,
			},
			message:      "payment，查昨天的",
			wantIntent:   intent.IntentMetricsQuery,
			wantService:  "payment",
			wantTime:     "yesterday",
			wantDecision: "proceed",
		},
		{
			name: "current turn overrides old slots",
			focus: session.SessionFocus{
				LastIntent: string(intent.IntentMetricsQuery),
				KnownSlots: map[string]string{
					"service": "checkout", "time_range": "last_15_minutes",
				},
			},
			message:      "换成 payment，看最近十分钟",
			wantIntent:   intent.IntentMetricsQuery,
			wantService:  "payment",
			wantTime:     "last_10_minutes",
			wantDecision: "proceed",
		},
		{
			name: "candidate reference",
			focus: session.SessionFocus{
				LastIntent: string(intent.IntentIncidentTriage),
				KnownSlots: map[string]string{},
				Candidates: []string{"checkout", "payment"},
			},
			message:      "第二个",
			wantIntent:   intent.IntentIncidentTriage,
			wantService:  "payment",
			wantTime:     "2026-07-31T07:40:00Z/2026-07-31T08:00:00Z",
			wantDecision: "proceed",
		},
		{
			name: "missing candidate reference",
			focus: session.SessionFocus{
				LastIntent: string(intent.IntentIncidentTriage),
				KnownSlots: map[string]string{},
			},
			message:      "第二个",
			wantIntent:   intent.IntentGeneralChat,
			wantDecision: "clarify",
			wantReason:   "AMBIGUOUS_REFERENCE",
		},
		{
			name: "new intent overrides old intent",
			focus: session.SessionFocus{
				LastIntent: string(intent.IntentIncidentTriage),
				KnownSlots: map[string]string{"service": "checkout"},
			},
			message:      "另外帮我找一下 runbook",
			wantIntent:   intent.IntentKnowledgeQuery,
			wantService:  "checkout",
			wantTime:     "2026-07-31T07:40:00Z/2026-07-31T08:00:00Z",
			wantDecision: "proceed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceSessionStore{focus: test.focus}
			graph := &recordingGraphRunner{}
			orchestrator := testOrchestrator(t)
			orchestrator.graph = graph
			service := NewService(orchestrator).WithSessionMemory(store)
			result, err := service.Execute(context.Background(), Command{
				RequestID:   "req-focus",
				SessionID:   "ses-focus",
				Message:     test.message,
				TimeContext: governanceTestTime(),
			})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if result.Output.Metadata["decision"] == "clarify" {
				if test.wantDecision != "clarify" ||
					result.Output.Metadata["reason_code"] != test.wantReason {
					t.Fatalf("metadata = %#v", result.Output.Metadata)
				}
				return
			}
			if test.wantDecision != "proceed" || graph.calls != 1 ||
				graph.input.Intent.Intent != test.wantIntent ||
				graph.input.Intent.Service != test.wantService {
				t.Fatalf("graph calls=%d input=%#v", graph.calls, graph.input)
			}
			actualTime := ""
			if graph.input.Intent.TimeRange != nil {
				actualTime = graph.input.Intent.TimeRange.Relative
			}
			if actualTime != test.wantTime {
				t.Fatalf("time range=%q want=%q", actualTime, test.wantTime)
			}
		})
	}
}

func TestServiceGovernanceTraceAndGeneralChatRules(t *testing.T) {
	tests := []struct {
		message      string
		wantDecision string
	}{
		{"分析 trace 4bf92f3577b34da6a3ce929d0e0e4736", "proceed"},
		{"分析 checkout 昨天的 trace", "proceed"},
		{"分析 trace", "clarify"},
		{"你好", "proceed"},
	}
	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			graph := &recordingGraphRunner{}
			orchestrator := testOrchestrator(t)
			orchestrator.graph = graph
			result, err := NewService(orchestrator).
				WithSessionMemory(&serviceSessionStore{}).
				Execute(context.Background(), Command{
					RequestID:   "req-rules",
					SessionID:   "ses-rules",
					Message:     test.message,
					TimeContext: governanceTestTime(),
				})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			actual := "proceed"
			if result.Output.Metadata["decision"] == "clarify" {
				actual = "clarify"
			}
			if actual != test.wantDecision {
				t.Fatalf("decision=%s metadata=%#v", actual, result.Output.Metadata)
			}
		})
	}
}

func TestServiceFocusFailureDegradesWithoutLeakingOrReplacingResult(t *testing.T) {
	store := &serviceSessionStore{
		focusErr: errors.New("redis password=do-not-leak"),
		saveErr:  errors.New("redis password=do-not-leak"),
	}
	graph := &recordingGraphRunner{}
	orchestrator := testOrchestrator(t)
	orchestrator.graph = graph
	result, err := NewService(orchestrator).
		WithSessionMemory(store).
		Execute(context.Background(), Command{
			RequestID:   "req-degraded",
			SessionID:   "ses-degraded",
			Message:     "查 checkout 错误率",
			TimeContext: governanceTestTime(),
		})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if graph.calls != 1 ||
		result.Output.Metadata["session_focus_available"] != false ||
		result.Output.Metadata["session_focus_persistence_failed"] != true {
		t.Fatalf("graph calls=%d metadata=%#v", graph.calls, result.Output.Metadata)
	}
	if strings.Contains(multiAgentAssistantMemoryContent(result.Output), "do-not-leak") {
		t.Fatalf("result leaked storage error: %#v", result.Output)
	}
}

func TestServiceClarificationFocusFailureStillReturnsClarification(t *testing.T) {
	store := &serviceSessionStore{saveErr: errors.New("redis unavailable")}
	graph := &recordingGraphRunner{}
	orchestrator := testOrchestrator(t)
	orchestrator.graph = graph
	result, err := NewService(orchestrator).
		WithSessionMemory(store).
		Execute(context.Background(), Command{
			RequestID:   "req-clarify-degraded",
			SessionID:   "ses-clarify-degraded",
			Message:     "帮我查一下错误率",
			TimeContext: governanceTestTime(),
		})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if graph.calls != 0 ||
		result.Output.Metadata["decision"] != "clarify" ||
		result.Output.Metadata["persistence_failed"] != true ||
		result.Output.FinalAnswer.Metadata["persistence_failed"] != true {
		t.Fatalf("graph calls=%d output=%#v", graph.calls, result.Output)
	}
}

func governanceTestTime() common.TimeRange {
	return common.TimeRange{
		From: "2026-07-31T07:40:00Z",
		To:   "2026-07-31T08:00:00Z",
	}
}

func testOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	orchestrator := NewOrchestrator(
		context.Background(),
		fakeTriagePlanner{},
		staticAnalyzer{finding: AgentFinding{
			Role:        AgentRoleEvidence,
			Summary:     "evidence summary",
			EvidenceIDs: []string{"evidence-1"},
			Evidence: []common.EvidenceItem{{
				ID:         "evidence-1",
				SourceType: "metrics",
				Content:    "checkout error rate elevated",
			}},
		}},
		staticAnalyzer{finding: AgentFinding{
			Role:        AgentRoleKnowledge,
			Summary:     "knowledge summary",
			EvidenceIDs: []string{"knowledge-1"},
			Evidence: []common.EvidenceItem{{
				ID:         "knowledge-1",
				SourceType: "knowledge",
				Content:    "checkout runbook",
			}},
		}},
		fakeSynthesizer{},
	)
	return orchestrator
}
