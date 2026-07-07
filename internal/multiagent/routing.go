package multiagent

import (
	"context"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type AgentPlan struct {
	Intent                intent.IntentResult             `json:"intent"`
	SelectedAgents        []AgentRole                     `json:"selected_agents"`
	SkippedAgents         []AgentRole                     `json:"skipped_agents"`
	RoleTools             map[AgentRole][]intent.ToolName `json:"role_tools"`
	RoleRAGHints          map[AgentRole]intent.RAGHints   `json:"role_rag_hints"`
	Metadata              map[string]any                  `json:"metadata,omitempty"`
	DynamicRoutingEnabled bool                            `json:"dynamic_routing_enabled"`
}

func PlanAgents(result intent.IntentResult) AgentPlan {
	return planAgents(result)
}

func planAgents(result intent.IntentResult) AgentPlan {
	result = intent.Normalize(result)
	dynamic := result.Intent != "" &&
		result.Intent != intent.IntentGeneralChat &&
		result.Confidence >= 0.55
	selected := RoleOrder()
	if dynamic {
		switch result.Intent {
		case intent.IntentIncidentTriage:
			selected = []AgentRole{
				AgentRoleTriage,
				AgentRoleEvidence,
				AgentRoleKnowledge,
				AgentRoleSynthesis,
			}
		case intent.IntentTraceAnalysis:
			selected = []AgentRole{
				AgentRoleEvidence,
				AgentRoleTriage,
				AgentRoleSynthesis,
			}
		case intent.IntentKnowledgeQuery, intent.IntentMitigation:
			selected = []AgentRole{AgentRoleKnowledge, AgentRoleSynthesis}
		case intent.IntentMetricsQuery, intent.IntentLogsQuery:
			selected = []AgentRole{AgentRoleEvidence, AgentRoleSynthesis}
		case intent.IntentStatusSummary:
			selected = []AgentRole{AgentRoleSynthesis}
		default:
			selected = []AgentRole{AgentRoleSynthesis}
		}
	}
	selected = normalizeAgentRoles(selected)
	skipped := skippedRoles(selected)
	return AgentPlan{
		Intent:                result,
		SelectedAgents:        selected,
		SkippedAgents:         skipped,
		RoleTools:             defaultRoleTools(),
		RoleRAGHints:          roleRAGHints(result),
		DynamicRoutingEnabled: dynamic,
		Metadata: map[string]any{
			"intent_type":             string(result.Intent),
			"selected_agents":         agentRoleStrings(selected),
			"skipped_agents":          agentRoleStrings(skipped),
			"dynamic_routing_enabled": dynamic,
		},
	}
}

func planAgentsWithTracing(
	ctx context.Context,
	result intent.IntentResult,
) AgentPlan {
	ctx, span := observability.StartSpan(
		ctx,
		"multiagent.intent.plan",
		attribute.String("intent.type", string(result.Intent)),
	)
	defer span.End()
	plan := PlanAgents(result)
	span.SetAttributes(
		attribute.Bool("dynamic_routing_enabled", plan.DynamicRoutingEnabled),
		attribute.Int("selected_agents_count", len(plan.SelectedAgents)),
		attribute.Int("skipped_agents_count", len(plan.SkippedAgents)),
		attribute.StringSlice("selected_agents", agentRoleStrings(plan.SelectedAgents)),
	)
	return plan
}

func (p AgentPlan) Selected(role AgentRole) bool {
	if len(p.SelectedAgents) == 0 {
		return true
	}
	for _, selected := range p.SelectedAgents {
		if selected == role {
			return true
		}
	}
	return false
}

func defaultRoleTools() map[AgentRole][]intent.ToolName {
	return map[AgentRole][]intent.ToolName{
		AgentRoleTriage: {},
		AgentRoleEvidence: {
			intent.ToolQueryMetrics,
			intent.ToolQueryLogs,
			intent.ToolQueryTraces,
		},
		AgentRoleKnowledge: {intent.ToolSearchKnowledge},
		AgentRoleSynthesis: {},
	}
}

func roleRAGHints(result intent.IntentResult) map[AgentRole]intent.RAGHints {
	base := result.RAGHints
	return map[AgentRole]intent.RAGHints{
		AgentRoleTriage: withCategories(
			base,
			[]string{"diagnosis", "playbook", "incident"},
		),
		AgentRoleEvidence: withCategories(
			base,
			[]string{"observability", "metrics", "logs", "traces"},
		),
		AgentRoleKnowledge: withCategories(
			base,
			[]string{"runbook", "incident", "playbook"},
		),
		AgentRoleSynthesis: base,
	}
}

func withCategories(
	hints intent.RAGHints,
	categories []string,
) intent.RAGHints {
	copy := hints
	copy.Categories = append([]string{}, categories...)
	return copy
}

func normalizeAgentRoles(roles []AgentRole) []AgentRole {
	allowed := map[AgentRole]struct{}{}
	for _, role := range RoleOrder() {
		allowed[role] = struct{}{}
	}
	result := make([]AgentRole, 0, len(roles))
	seen := map[AgentRole]struct{}{}
	for _, role := range roles {
		if _, ok := allowed[role]; !ok {
			continue
		}
		if _, exists := seen[role]; exists {
			continue
		}
		seen[role] = struct{}{}
		result = append(result, role)
	}
	if len(result) == 0 {
		return RoleOrder()
	}
	return result
}

func skippedRoles(selected []AgentRole) []AgentRole {
	selectedSet := map[AgentRole]struct{}{}
	for _, role := range selected {
		selectedSet[role] = struct{}{}
	}
	result := []AgentRole{}
	for _, role := range RoleOrder() {
		if _, exists := selectedSet[role]; !exists {
			result = append(result, role)
		}
	}
	return result
}

func agentRoleStrings(roles []AgentRole) []string {
	result := make([]string, 0, len(roles))
	for _, role := range roles {
		result = append(result, string(role))
	}
	return result
}
