package multiagent

import (
	"context"
	"sync"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestServiceStreamEmitsRoleAndEvidenceProgress(t *testing.T) {
	var (
		seenMu sync.Mutex
		seen   []StreamEvent
		plans  []TriagePlan
	)
	orchestrator := NewOrchestrator(
		context.Background(),
		fakeTriagePlanner{},
		recordingAnalyzer{
			role: AgentRoleEvidence,
			mu:   &seenMu,
			seen: &plans,
		},
		recordingAnalyzer{
			role: AgentRoleKnowledge,
			mu:   &seenMu,
			seen: &plans,
		},
		NewSynthesisAgent(nil),
	)
	service := NewService(orchestrator)
	_, err := service.Stream(
		context.Background(),
		Command{
			RequestID: "req-stream",
			SessionID: "session-stream",
			Message:   "inspect checkout",
			TimeContext: common.TimeRange{
				From: "2026-07-03T00:00:00Z",
				To:   "2026-07-03T00:20:00Z",
			},
		},
		func(event StreamEvent) {
			seenMu.Lock()
			defer seenMu.Unlock()
			seen = append(seen, event)
		},
	)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	counts := map[string]int{}
	roles := map[string]bool{}
	for _, event := range seen {
		counts[event.Type]++
		if role, ok := event.Data["agent_role"].(string); ok {
			roles[role] = true
		}
	}
	if counts["multi_agent_started"] != 1 ||
		counts["intent_recognized"] != 1 ||
		counts["multiagent_plan_created"] != 1 ||
		counts["agent_step_started"] != 5 ||
		counts["agent_step_completed"] != 5 ||
		counts["synthesis_started"] != 1 ||
		counts["evidence_collected"] != 1 {
		t.Fatalf("event counts = %#v", counts)
	}
	for _, role := range []string{
		"triage",
		"evidence",
		"knowledge",
		"merge",
		"synthesis",
	} {
		if !roles[role] {
			t.Fatalf("missing role %q in events: %#v", role, roles)
		}
	}
}

func TestServiceStreamClarificationSkipsExecutionEvents(t *testing.T) {
	recognizer := &countingRecognizer{}
	graph := &recordingGraphRunner{}
	orchestrator := testOrchestrator(t)
	orchestrator.graph = graph
	service := NewService(orchestrator).
		WithIntentRecognizer(recognizer).
		WithSessionMemory(&serviceSessionStore{})
	events := []string{}

	result, err := service.Stream(
		context.Background(),
		Command{
			RequestID:   "req-stream-clarify",
			SessionID:   "ses-stream-clarify",
			Message:     "帮我查一下错误率",
			TimeContext: governanceTestTime(),
		},
		func(event StreamEvent) {
			events = append(events, event.Type)
		},
	)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if recognizer.calls != 1 || graph.calls != 0 ||
		result.Output.Metadata["decision"] != string(intent.DecisionClarify) {
		t.Fatalf(
			"recognizer calls=%d graph calls=%d metadata=%#v",
			recognizer.calls,
			graph.calls,
			result.Output.Metadata,
		)
	}
	for _, required := range []string{
		"multi_agent_started",
		"intent_recognized",
		"slot_validation_completed",
		"clarification_required",
	} {
		if !containsEvent(events, required) {
			t.Fatalf("events=%v missing %q", events, required)
		}
	}
	for _, forbidden := range []string{
		"multiagent_plan_created",
		"role_rag_started",
		"triage_started",
		"agent_step_started",
		"tool_call_started",
		"tool_started",
		"tool_completed",
		"evidence_collected",
		"synthesis_started",
	} {
		if containsEvent(events, forbidden) {
			t.Fatalf("events=%v contain forbidden %q", events, forbidden)
		}
	}
}

func containsEvent(events []string, expected string) bool {
	for _, event := range events {
		if event == expected {
			return true
		}
	}
	return false
}
