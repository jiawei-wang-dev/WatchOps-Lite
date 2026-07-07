package intent

import (
	"strings"
	"testing"
)

func TestSelectSkillsForToolsFiltersByToolNames(t *testing.T) {
	all := []SkillCard{
		{Name: "metric_inspection", Description: "metrics", ToolNames: []string{"query_metrics"}},
		{Name: "log_investigation", Description: "logs", ToolNames: []string{"query_logs"}},
		{Name: "runbook_lookup", Description: "knowledge", ToolNames: []string{"search_knowledge"}},
	}

	selected := SelectSkillsForTools(
		all,
		[]ToolName{ToolQueryMetrics, ToolQueryLogs},
		IntentResult{Intent: IntentMetricsQuery, Confidence: 0.9},
	)

	if len(selected) != 2 ||
		selected[0].Name != "metric_inspection" ||
		selected[1].Name != "log_investigation" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestFormatRoleSkillCardsIncludesRoleInstruction(t *testing.T) {
	card := FormatRoleSkillCards("synthesis", []SkillCard{{
		Name:        "evidence_synthesis",
		Description: "consume existing evidence only.",
	}})

	if !strings.Contains(card, "Available role-specific diagnostic skills:") ||
		!strings.Contains(card, "evidence_synthesis") ||
		!strings.Contains(card, "do not call new tools") {
		t.Fatalf("card = %q", card)
	}
}
