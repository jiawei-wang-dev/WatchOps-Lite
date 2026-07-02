package policy

import (
	"context"
	"testing"
)

func TestPlanRanksLowLatencyRelevantToolsFirst(t *testing.T) {
	plan := New().Plan(context.Background(), Request{
		Query: "Why did checkout error rate increase after timeout errors?",
		Context: AgentContext{Stats: map[string]ToolStats{
			ToolMetrics: {
				HistoricalLatencyMS: 20,
				SuccessRate:         0.99,
				FallbackFrequency:   0.01,
				RelativeCost:        0.1,
			},
			ToolLogs: {
				HistoricalLatencyMS: 600,
				SuccessRate:         0.85,
				FallbackFrequency:   0.2,
				RelativeCost:        0.4,
			},
		}},
	})

	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %#v, want metrics and logs", plan.Steps)
	}
	if plan.Steps[0].Tool != ToolMetrics || plan.Steps[1].Tool != ToolLogs {
		t.Fatalf("steps = %#v, want low-latency metrics before logs", plan.Steps)
	}
	if plan.Fallback.AllowMockFallback {
		t.Fatalf("fallback = %#v, must be deferred before real failures", plan.Fallback)
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

func TestPlanAllowsMockOnlyAfterEveryRealToolFails(t *testing.T) {
	policy := New()
	request := Request{
		Query: "Why did checkout error rate increase after timeout errors?",
		Context: AgentContext{RealSourceFailures: map[string]bool{
			ToolMetrics: true,
		}},
	}
	if plan := policy.Plan(context.Background(), request); plan.Fallback.AllowMockFallback {
		t.Fatalf("fallback = %#v, want deferred fallback", plan.Fallback)
	}

	request.Context.RealSourceFailures[ToolLogs] = true
	plan := policy.Plan(context.Background(), request)
	if !plan.Fallback.AllowMockFallback ||
		len(plan.Fallback.FailedRealSources) != len(plan.Steps) {
		t.Fatalf("fallback = %#v, want fallback after all real tools fail", plan.Fallback)
	}
}

func TestUnknownQueryProducesNoToolPlan(t *testing.T) {
	plan := New().Plan(context.Background(), Request{Query: "hello"})
	if plan.QueryType != QueryUnknown || len(plan.Steps) != 0 {
		t.Fatalf("plan = %#v, want unknown empty plan", plan)
	}
}
