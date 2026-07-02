package policy

import (
	"context"
	"testing"
)

func TestPlanProvidesSimpleRelevantToolOrder(t *testing.T) {
	plan := New().Plan(context.Background(), Request{
		Query: "Why did checkout error rate increase after timeout errors?",
	})

	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %#v, want metrics and logs", plan.Steps)
	}
	if plan.Steps[0].Tool != ToolMetrics || plan.Steps[1].Tool != ToolLogs {
		t.Fatalf("steps = %#v, want stable metrics-before-logs hints", plan.Steps)
	}
	if plan.Fallback.AllowMockFallback {
		t.Fatalf("fallback = %#v, policy must not control runtime fallback", plan.Fallback)
	}
}

func TestPlanAvoidsExpensiveUnneededTools(t *testing.T) {
	plan := New().Plan(context.Background(), Request{
		Query: "Show checkout error rate metrics",
	})

	for _, step := range plan.Steps {
		if step.Tool == ToolTraces || step.Tool == ToolKnowledge {
			t.Fatalf("plan = %#v, includes an unnecessary expensive tool", plan)
		}
	}
}

func TestPlanNeverControlsMockFallback(t *testing.T) {
	policy := New()
	request := Request{
		Query: "Why did checkout error rate increase after timeout errors?",
		Context: AgentContext{RealSourceFailures: map[string]bool{
			ToolMetrics: true,
		}},
	}
	request.Context.RealSourceFailures[ToolLogs] = true
	plan := policy.Plan(context.Background(), request)
	if plan.Fallback.AllowMockFallback ||
		plan.Fallback.Condition != "owned_by_tool_runtime" {
		t.Fatalf("fallback = %#v, want Tool Runtime ownership", plan.Fallback)
	}
}

func TestUnknownQueryProducesNoToolPlan(t *testing.T) {
	plan := New().Plan(context.Background(), Request{Query: "hello"})
	if plan.QueryType != QueryUnknown || len(plan.Steps) != 0 {
		t.Fatalf("plan = %#v, want unknown empty plan", plan)
	}
}
