package intent

func SelectAgentsForIntent(result IntentResult) []AgentRole {
	return defaultAgents(result.Intent, result.SuggestedAgents)
}

func RoleSelected(result IntentResult, role AgentRole) bool {
	roles := SelectAgentsForIntent(result)
	if len(roles) == 0 {
		return true
	}
	for _, selected := range roles {
		if selected == role {
			return true
		}
	}
	return false
}
