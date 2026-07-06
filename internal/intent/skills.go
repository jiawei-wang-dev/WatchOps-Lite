package intent

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
