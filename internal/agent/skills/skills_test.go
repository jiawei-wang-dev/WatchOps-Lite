package skills

import (
	"reflect"
	"strings"
	"testing"
)

func TestDiagnosticSkillsDescribeExistingTools(t *testing.T) {
	tests := []struct {
		skill Skill
		tools []string
	}{
		{MetricInspectionSkill(), []string{"query_metrics"}},
		{LogInvestigationSkill(), []string{"query_logs"}},
		{TraceInspectionSkill(), []string{"query_traces"}},
		{RunbookLookupSkill(), []string{"search_knowledge"}},
		{
			CheckoutIncidentDiagnosisSkill(),
			[]string{"query_metrics", "query_logs", "query_traces", "search_knowledge", "query_alerts", "get_service_topology"},
		},
	}

	for _, test := range tests {
		if test.skill.Name() == "" || test.skill.Description() == "" {
			t.Fatalf("skill = %#v, want name and description", test.skill)
		}
		for _, tool := range test.tools {
			if !strings.Contains(test.skill.Description(), tool) &&
				test.skill.Name() != "checkout_incident_diagnosis" {
				t.Fatalf("%s description = %q, want mention of %s", test.skill.Name(), test.skill.Description(), tool)
			}
		}
		if got := test.skill.ToolNames(); !reflect.DeepEqual(got, test.tools) {
			t.Fatalf("%s tools = %#v, want %#v", test.skill.Name(), got, test.tools)
		}
	}
}

func TestToolNamesReturnsCopy(t *testing.T) {
	skill := CheckoutIncidentDiagnosisSkill()
	names := skill.ToolNames()
	names[0] = "changed"

	if skill.ToolNames()[0] != "query_metrics" {
		t.Fatalf("ToolNames() exposed mutable skill state")
	}
}

func TestAuxiliaryToolsOnlyAppearInCheckoutSkill(t *testing.T) {
	coreSkills := []Skill{
		MetricInspectionSkill(),
		LogInvestigationSkill(),
		TraceInspectionSkill(),
		RunbookLookupSkill(),
	}
	for _, skill := range coreSkills {
		description := skill.Description()
		if strings.Contains(description, "query_alerts") ||
			strings.Contains(description, "get_service_topology") {
			t.Fatalf("%s description should not mention auxiliary tools: %q", skill.Name(), description)
		}
	}

	checkout := CheckoutIncidentDiagnosisSkill()
	if !strings.Contains(checkout.Description(), "optionally use query_alerts") ||
		!strings.Contains(checkout.Description(), "get_service_topology") {
		t.Fatalf("checkout description = %q, want optional auxiliary context", checkout.Description())
	}
}
