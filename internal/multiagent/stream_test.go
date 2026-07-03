package multiagent

import (
	"context"
	"sync"
	"testing"

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
