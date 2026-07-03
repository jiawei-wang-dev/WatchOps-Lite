package multiagent

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestMergeAgentFindingsDeduplicatesEvidenceStably(t *testing.T) {
	duplicate := common.EvidenceItem{
		ID:         "shared-1",
		SourceType: "metrics",
		Content:    "first copy",
	}
	merged := MergeAgentFindings(
		TriagePlan{Limitations: []agenteino.Limitation{{
			Code:    "PLAN_LIMIT",
			Message: "plan limitation",
		}}},
		AgentFinding{
			Evidence: []common.EvidenceItem{
				duplicate,
				{ID: "logs-1", SourceType: "logs", Content: "log"},
			},
			ToolRuns: []agenteino.ToolRun{{Tool: "query_metrics"}},
			Limitations: []agenteino.Limitation{{
				Code:    "SHARED_LIMIT",
				Message: "same limitation",
			}},
		},
		AgentFinding{
			Evidence: []common.EvidenceItem{
				{ID: "shared-1", SourceType: "knowledge", Content: "second copy"},
				{ID: "knowledge-1", SourceType: "knowledge", Content: "runbook"},
			},
			ToolRuns: []agenteino.ToolRun{{Tool: "search_knowledge"}},
			Limitations: []agenteino.Limitation{{
				Code:    "SHARED_LIMIT",
				Message: "same limitation",
			}},
		},
	)

	wantIDs := []string{"shared-1", "logs-1", "knowledge-1"}
	if !reflect.DeepEqual(merged.EvidenceIDs, wantIDs) {
		t.Fatalf("EvidenceIDs = %#v, want %#v", merged.EvidenceIDs, wantIDs)
	}
	if merged.Evidence[0].Content != "first copy" {
		t.Fatalf("duplicate ordering changed: %#v", merged.Evidence[0])
	}
	if len(merged.ToolRuns) != 2 || len(merged.Limitations) != 2 {
		t.Fatalf("merged = %#v", merged)
	}
}

type failingAnalyzer struct{}

func (failingAnalyzer) Analyze(
	context.Context,
	TriagePlan,
) (AgentFinding, error) {
	return AgentFinding{}, errors.New("dependency failed")
}

func TestOrchestratorContinuesWhenOneFindingAgentFails(t *testing.T) {
	var seen []TriagePlan
	orchestrator := NewOrchestrator(
		context.Background(),
		fakeTriagePlanner{},
		failingAnalyzer{},
		recordingAnalyzer{
			role: AgentRoleKnowledge,
			mu:   newNoopMutex(),
			seen: &seen,
		},
		fakeSynthesizer{},
	)

	result, err := orchestrator.Execute(context.Background(), Input{
		SessionID: "session-1",
		Message:   "checkout error rate",
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Steps[1].Status != AgentStepFailed ||
		result.Steps[2].Status != AgentStepCompleted {
		t.Fatalf("Steps = %#v", result.Steps)
	}
	foundFailure := false
	for _, limitation := range result.FinalAnswer.Limitations {
		if limitation.Code == "EVIDENCE_AGENT_FAILED" {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatalf("Limitations = %#v", result.FinalAnswer.Limitations)
	}
}

func newNoopMutex() *sync.Mutex {
	return &sync.Mutex{}
}
