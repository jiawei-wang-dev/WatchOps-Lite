package multiagent

import (
	"reflect"
	"strings"
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

func TestPlanAgentsBuildsRoleSpecificSkillCards(t *testing.T) {
	plan := PlanAgents(intent.IntentResult{
		Intent:     intent.IntentIncidentTriage,
		Confidence: 0.9,
		Source:     "rule",
	})

	evidenceCard := plan.RoleSkillCards[AgentRoleEvidence]
	knowledgeCard := plan.RoleSkillCards[AgentRoleKnowledge]
	synthesisCard := plan.RoleSkillCards[AgentRoleSynthesis]
	if evidenceCard == "" || knowledgeCard == "" || synthesisCard == "" {
		t.Fatalf("role skill cards = %#v", plan.RoleSkillCards)
	}
	if !strings.Contains(evidenceCard, "metric_inspection") ||
		!strings.Contains(evidenceCard, "log_investigation") ||
		!strings.Contains(evidenceCard, "trace_inspection") {
		t.Fatalf("evidence card = %q", evidenceCard)
	}
	if !strings.Contains(knowledgeCard, "runbook_lookup") {
		t.Fatalf("knowledge card = %q", knowledgeCard)
	}
	if !strings.Contains(synthesisCard, evidenceSynthesisSkillName) ||
		strings.Contains(synthesisCard, "metric_inspection") {
		t.Fatalf("synthesis card = %q", synthesisCard)
	}
	if evidenceCard == knowledgeCard || knowledgeCard == synthesisCard {
		t.Fatalf("role cards should be role-specific: %#v", plan.RoleSkillCards)
	}
}

func TestPlanAgentsSkippedRolesStillHaveSafeSkillMetadata(t *testing.T) {
	plan := PlanAgents(intent.IntentResult{
		Intent:     intent.IntentKnowledgeQuery,
		Confidence: 0.9,
		Source:     "rule",
	})

	if !plan.Selected(AgentRoleKnowledge) || plan.Selected(AgentRoleEvidence) {
		t.Fatalf("selected agents = %#v", plan.SelectedAgents)
	}
	if len(plan.RoleSkillHints[AgentRoleKnowledge]) == 0 ||
		plan.RoleSkillCards[AgentRoleKnowledge] == "" {
		t.Fatalf("knowledge role skills missing: %#v", plan.RoleSkillCards)
	}
	if _, ok := plan.RoleSkillCards[AgentRoleEvidence]; !ok {
		t.Fatalf("skipped evidence role should still have non-panicking defaults")
	}
}
