package multiagent

import (
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/skills"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

const evidenceSynthesisSkillName = "evidence_synthesis"

func buildRoleSkillHints(
	result intent.IntentResult,
	roleTools map[AgentRole][]intent.ToolName,
) map[AgentRole][]intent.SkillCard {
	all := multiAgentSkillDefinitions()
	hints := map[AgentRole][]intent.SkillCard{}
	for _, role := range RoleOrder() {
		selected := intent.SelectSkillsForTools(
			all,
			roleTools[role],
			result,
		)
		selected = appendRoleSpecificSkills(selected, all, role)
		hints[role] = boundedRoleSkills(selected, role)
	}
	return hints
}

func buildRoleSkillCards(
	hints map[AgentRole][]intent.SkillCard,
) map[AgentRole]string {
	cards := map[AgentRole]string{}
	for _, role := range RoleOrder() {
		card := strings.TrimSpace(
			intent.FormatRoleSkillCards(string(role), hints[role]),
		)
		if card != "" {
			cards[role] = card
		}
	}
	return cards
}

func multiAgentSkillDefinitions() []intent.SkillCard {
	definitions := []skills.Skill{
		skills.MetricInspectionSkill(),
		skills.LogInvestigationSkill(),
		skills.TraceInspectionSkill(),
		skills.RunbookLookupSkill(),
		skills.CheckoutIncidentDiagnosisSkill(),
	}
	cards := make([]intent.SkillCard, 0, len(definitions)+1)
	for _, definition := range definitions {
		cards = append(cards, intent.SkillCard{
			Name:        definition.Name(),
			Description: definition.Description(),
			ToolNames:   definition.ToolNames(),
		})
	}
	cards = append(cards, intent.SkillCard{
		Name:        evidenceSynthesisSkillName,
		Description: "consume findings, processed evidence citations, and hypothesis evaluation; report limitations and do not invent missing metrics, logs, or traces.",
	})
	return cards
}

func appendRoleSpecificSkills(
	selected []intent.SkillCard,
	all []intent.SkillCard,
	role AgentRole,
) []intent.SkillCard {
	switch role {
	case AgentRoleTriage:
		selected = appendSkillByName(selected, all, "checkout_incident_diagnosis")
		selected = appendSkillByName(selected, all, "runbook_lookup")
	case AgentRoleEvidence:
		selected = appendSkillByName(selected, all, "metric_inspection")
		selected = appendSkillByName(selected, all, "log_investigation")
		selected = appendSkillByName(selected, all, "trace_inspection")
		selected = appendSkillByName(selected, all, "checkout_incident_diagnosis")
	case AgentRoleKnowledge:
		selected = appendSkillByName(selected, all, "runbook_lookup")
	case AgentRoleSynthesis:
		selected = appendSkillByName(selected, all, evidenceSynthesisSkillName)
	}
	return selected
}

func appendSkillByName(
	selected []intent.SkillCard,
	all []intent.SkillCard,
	name string,
) []intent.SkillCard {
	for _, current := range selected {
		if current.Name == name {
			return selected
		}
	}
	for _, card := range all {
		if card.Name == name {
			return append(selected, card)
		}
	}
	return selected
}

func boundedRoleSkills(cards []intent.SkillCard, role AgentRole) []intent.SkillCard {
	limit := 4
	if role == AgentRoleKnowledge || role == AgentRoleSynthesis {
		limit = 2
	}
	if len(cards) <= limit {
		return cards
	}
	return cards[:limit]
}

func roleSkillNames(hints map[AgentRole][]intent.SkillCard) map[string][]string {
	result := map[string][]string{}
	for role := range hints {
		result[string(role)] = roleSkillNamesForRole(hints, role)
	}
	return result
}

func roleSkillNamesForRole(
	hints map[AgentRole][]intent.SkillCard,
	role AgentRole,
) []string {
	names := make([]string, 0, len(hints[role]))
	for _, card := range hints[role] {
		if strings.TrimSpace(card.Name) != "" {
			names = append(names, card.Name)
		}
	}
	return names
}
