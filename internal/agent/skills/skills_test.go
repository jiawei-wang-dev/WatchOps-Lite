package skills

import (
	"reflect"
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
			[]string{"query_metrics", "query_logs", "query_traces", "search_knowledge"},
		},
	}

	for _, test := range tests {
		if test.skill.Name() == "" || test.skill.Description() == "" {
			t.Fatalf("skill = %#v, want name and description", test.skill)
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
