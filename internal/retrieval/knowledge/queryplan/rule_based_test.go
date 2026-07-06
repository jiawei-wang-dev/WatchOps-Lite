package queryplan

import (
	"context"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

func TestRuleBasedPlannerExpandsErrorQuery(t *testing.T) {
	planner := NewRuleBasedPlanner()

	plan, err := planner.Plan(context.Background(), QueryPlanInput{
		UserMessage: "checkout 最近老 500",
		Intent: intent.IntentResult{
			Intent:   intent.IntentIncidentTriage,
			Service:  "checkout-service",
			Symptom:  "error",
			Keywords: []string{"500"},
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	types := queryTypeSet(plan.Queries)
	for _, expected := range []QueryType{
		QueryOriginal,
		QueryCanonical,
		QuerySynonym,
		QueryDiagnostic,
		QueryStepBack,
	} {
		if !types[expected] {
			t.Fatalf("query types = %#v, missing %q", plan.Queries, expected)
		}
	}
	if plan.Metadata["query_rewrite_applied"] != true {
		t.Fatalf("metadata = %#v, want query rewrite applied", plan.Metadata)
	}
}

func TestRuleBasedPlannerUnknownMessageKeepsOriginal(t *testing.T) {
	planner := NewRuleBasedPlanner()

	plan, err := planner.Plan(context.Background(), QueryPlanInput{
		UserMessage: "hello",
		Intent: intent.IntentResult{
			Intent: intent.IntentGeneralChat,
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Queries) == 0 ||
		plan.Queries[0].Type != QueryOriginal ||
		plan.Queries[0].Query != "hello" {
		t.Fatalf("plan = %#v, want original query preserved", plan)
	}
}

func queryTypeSet(queries []RAGSubQuery) map[QueryType]bool {
	result := map[QueryType]bool{}
	for _, query := range queries {
		result[query.Type] = true
	}
	return result
}
