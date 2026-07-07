package intent

import (
	"fmt"
	"strings"
)

type SkillCard struct {
	Name        string
	Description string
	ToolNames   []string
}

func SelectSkillsForIntent(all []SkillCard, result IntentResult) []SkillCard {
	if len(all) == 0 {
		return []SkillCard{}
	}
	if result.Intent == "" || result.Confidence < 0.55 || len(result.SuggestedTools) == 0 {
		return append([]SkillCard{}, all...)
	}
	wanted := map[string]struct{}{}
	for _, tool := range result.SuggestedTools {
		wanted[string(tool)] = struct{}{}
	}
	selected := make([]SkillCard, 0, len(all))
	for _, card := range all {
		if skillMatchesTools(card, wanted) {
			selected = append(selected, card)
		}
	}
	if len(selected) == 0 {
		return append([]SkillCard{}, all...)
	}
	if result.Intent == IntentIncidentTriage {
		selected = appendMissingSkill(selected, all, ToolQueryMetrics)
		selected = appendMissingSkill(selected, all, ToolQueryLogs)
		selected = appendMissingSkill(selected, all, ToolSearchKnowledge)
	}
	return selected
}

func SelectSkillsForTools(
	all []SkillCard,
	tools []ToolName,
	result IntentResult,
) []SkillCard {
	if len(all) == 0 {
		return []SkillCard{}
	}
	if len(tools) == 0 {
		return []SkillCard{}
	}
	wanted := map[string]struct{}{}
	for _, tool := range tools {
		if tool == "" {
			continue
		}
		wanted[string(tool)] = struct{}{}
	}
	selected := make([]SkillCard, 0, len(all))
	for _, card := range all {
		if skillMatchesTools(card, wanted) {
			selected = appendSkillCard(selected, card)
		}
	}
	if result.Intent == IntentIncidentTriage {
		for _, name := range []ToolName{
			ToolQueryMetrics,
			ToolQueryLogs,
			ToolQueryTraces,
			ToolSearchKnowledge,
		} {
			if _, ok := wanted[string(name)]; ok {
				selected = appendMissingSkill(selected, all, name)
			}
		}
	}
	return selected
}

func FormatRoleSkillCards(role string, hints []SkillCard) string {
	role = strings.TrimSpace(role)
	if len(hints) == 0 {
		return roleInstruction(role)
	}
	lines := []string{"Available role-specific diagnostic skills:"}
	for _, hint := range boundedSkillCards(hints, 4) {
		name := strings.TrimSpace(hint.Name)
		description := strings.TrimSpace(hint.Description)
		if name == "" || description == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", name, description))
	}
	if instruction := roleInstruction(role); instruction != "" {
		lines = append(lines, "Role instruction: "+instruction)
	}
	if len(lines) == 1 {
		return roleInstruction(role)
	}
	return strings.Join(lines, "\n")
}

func skillMatchesTools(card SkillCard, tools map[string]struct{}) bool {
	for _, tool := range card.ToolNames {
		if _, exists := tools[tool]; exists {
			return true
		}
	}
	return false
}

func appendMissingSkill(selected []SkillCard, all []SkillCard, tool ToolName) []SkillCard {
	for _, card := range selected {
		for _, name := range card.ToolNames {
			if name == string(tool) {
				return selected
			}
		}
	}
	for _, card := range all {
		for _, name := range card.ToolNames {
			if name == string(tool) {
				return append(selected, card)
			}
		}
	}
	return selected
}

func appendSkillCard(selected []SkillCard, card SkillCard) []SkillCard {
	for _, current := range selected {
		if current.Name == card.Name {
			return selected
		}
	}
	return append(selected, card)
}

func boundedSkillCards(cards []SkillCard, limit int) []SkillCard {
	if limit <= 0 || len(cards) <= limit {
		return cards
	}
	return cards[:limit]
}

func roleInstruction(role string) string {
	switch role {
	case "triage":
		return "Identify service, symptom, and time window; create an investigation plan and candidate hypotheses without making final root-cause claims."
	case "evidence":
		return "Collect verifiable runtime evidence. If traces, logs, or metrics are missing, report a limitation instead of inventing observations."
	case "knowledge":
		return "Use runbooks and memory as supporting guidance only; do not conclude the current root cause from knowledge alone."
	case "synthesis":
		return "Consume findings, processed evidence citations, and hypothesis evaluation; do not call new tools or invent missing evidence."
	default:
		return ""
	}
}
