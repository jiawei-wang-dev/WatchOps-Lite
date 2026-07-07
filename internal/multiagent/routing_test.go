package multiagent

import (
	"reflect"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

func TestPlanAgentsIncidentTriageSelectsAllRoles(t *testing.T) {
	plan := PlanAgents(intent.IntentResult{
		Intent:     intent.IntentIncidentTriage,
		Confidence: 0.9,
		Source:     "rule",
	})

	if !reflect.DeepEqual(plan.SelectedAgents, RoleOrder()) ||
		len(plan.SkippedAgents) != 0 ||
		!plan.DynamicRoutingEnabled {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanAgentsKnowledgeQuerySelectsKnowledgeAndSynthesis(t *testing.T) {
	plan := PlanAgents(intent.IntentResult{
		Intent:     intent.IntentKnowledgeQuery,
		Confidence: 0.9,
		Source:     "rule",
	})

	want := []AgentRole{AgentRoleKnowledge, AgentRoleSynthesis}
	if !reflect.DeepEqual(plan.SelectedAgents, want) ||
		plan.Selected(AgentRoleEvidence) ||
		!plan.Selected(AgentRoleKnowledge) ||
		!plan.Selected(AgentRoleSynthesis) {
		t.Fatalf("plan = %#v, want knowledge+synthesis", plan)
	}
	if !reflect.DeepEqual(plan.SkippedAgents, []AgentRole{AgentRoleTriage, AgentRoleEvidence}) {
		t.Fatalf("skipped = %#v", plan.SkippedAgents)
	}
}

func TestPlanAgentsTraceAnalysisSelectsEvidenceTriageSynthesis(t *testing.T) {
	plan := PlanAgents(intent.IntentResult{
		Intent:     intent.IntentTraceAnalysis,
		Confidence: 0.9,
		Source:     "rule",
	})

	want := []AgentRole{
		AgentRoleEvidence,
		AgentRoleTriage,
		AgentRoleSynthesis,
	}
	if !reflect.DeepEqual(plan.SelectedAgents, want) ||
		plan.Selected(AgentRoleKnowledge) {
		t.Fatalf("plan = %#v, want evidence+triage+synthesis", plan)
	}
}

func TestPlanAgentsLowConfidenceKeepsLegacyAllRoles(t *testing.T) {
	plan := PlanAgents(intent.IntentResult{
		Intent:     intent.IntentKnowledgeQuery,
		Confidence: 0.3,
		Source:     "rule",
	})

	if !reflect.DeepEqual(plan.SelectedAgents, RoleOrder()) ||
		len(plan.SkippedAgents) != 0 ||
		plan.DynamicRoutingEnabled {
		t.Fatalf("plan = %#v, want legacy all-role fallback", plan)
	}
}

func TestPlanAgentsRoleToolsAndRAGHintsAreRoleAware(t *testing.T) {
	plan := PlanAgents(intent.IntentResult{
		Intent:     intent.IntentIncidentTriage,
		Confidence: 0.9,
		Source:     "rule",
	})

	if len(plan.RoleTools[AgentRoleEvidence]) != 3 ||
		len(plan.RoleTools[AgentRoleKnowledge]) != 1 ||
		plan.RoleTools[AgentRoleKnowledge][0] != intent.ToolSearchKnowledge {
		t.Fatalf("role tools = %#v", plan.RoleTools)
	}
	if len(plan.RoleRAGHints[AgentRoleKnowledge].Categories) == 0 ||
		len(plan.RoleRAGHints[AgentRoleEvidence].Categories) == 0 {
		t.Fatalf("role rag hints = %#v", plan.RoleRAGHints)
	}
}
