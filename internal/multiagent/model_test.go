package multiagent

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestRoleOrderIsStable(t *testing.T) {
	want := []AgentRole{
		AgentRoleTriage,
		AgentRoleEvidence,
		AgentRoleKnowledge,
		AgentRoleSynthesis,
	}
	if got := RoleOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RoleOrder() = %#v, want %#v", got, want)
	}
}

func TestDomainModelReusesExistingContracts(t *testing.T) {
	score := 0.9
	result := MultiAgentResult{
		Steps: []AgentStep{{
			Role:        AgentRoleEvidence,
			Name:        "Evidence Agent",
			Status:      AgentStepCompleted,
			EvidenceIDs: []string{"metric-1"},
			ToolRuns: []agenteino.ToolRun{{
				Tool:          "query_metrics",
				Success:       true,
				EvidenceCount: 1,
			}},
			StartedAt:   time.Unix(1, 0).UTC(),
			CompletedAt: time.Unix(2, 0).UTC(),
			DurationMS:  1000,
		}},
		Evidence: []common.EvidenceItem{{
			ID:         "metric-1",
			SourceType: "metrics",
			SourceName: "prometheus",
			Content:    "checkout error rate is elevated",
			Score:      &score,
		}},
		FinalAnswer: agenteino.AgentOutput{
			Conclusions: []agenteino.Conclusion{{
				Text:        "The error rate is elevated.",
				EvidenceIDs: []string{"metric-1"},
			}},
		},
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("Marshal() returned no data")
	}
	if result.Evidence[0].ID != result.Steps[0].EvidenceIDs[0] {
		t.Fatal("step does not reference the reused evidence contract")
	}
}

func TestTriagePlanUsesBoundedExistingTimeRange(t *testing.T) {
	plan := TriagePlan{
		Service:      "checkout",
		IncidentType: "high_error_rate",
		EvidencePlan: []string{"metrics", "logs", "knowledge"},
		Query:        "checkout error rate",
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
		Language: "en",
	}

	if err := plan.TimeContext.Validate(); err != nil {
		t.Fatalf("TimeContext.Validate() error = %v", err)
	}
	if len(plan.EvidencePlan) != 3 {
		t.Fatalf("EvidencePlan length = %d, want 3", len(plan.EvidencePlan))
	}
}
