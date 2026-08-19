package queryplan

import (
	"context"
	"strings"
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

func TestMultiQueryDecisionUsesOnlyComplexIncidents(t *testing.T) {
	tests := []struct {
		name  string
		input QueryPlanInput
		want  bool
	}{
		{"incident dependency", QueryPlanInput{UserMessage: "checkout 被 payment 延迟拖慢", Intent: intent.IntentResult{Intent: intent.IntentIncidentTriage}}, true},
		{"metrics", QueryPlanInput{UserMessage: "checkout error rate", Intent: intent.IntentResult{Intent: intent.IntentMetricsQuery, Service: "checkout"}}, false},
		{"logs", QueryPlanInput{UserMessage: "checkout logs", Intent: intent.IntentResult{Intent: intent.IntentLogsQuery, Service: "checkout"}}, false},
		{"trace id", QueryPlanInput{UserMessage: "trace", Intent: intent.IntentResult{Intent: intent.IntentTraceAnalysis, TraceID: "4bf92f3577b34da6"}}, false},
		{"knowledge", QueryPlanInput{UserMessage: "payment runbook", Intent: intent.IntentResult{Intent: intent.IntentKnowledgeQuery}}, false},
		{"simple incident", QueryPlanInput{UserMessage: "orders connection errors，查日志和 runbook", Intent: intent.IntentResult{Intent: intent.IntentIncidentTriage}}, false},
		{"correction", QueryPlanInput{UserMessage: "不是 payment，是 checkout", Service: "checkout", Intent: intent.IntentResult{Intent: intent.IntentIncidentTriage, Service: "checkout"}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ShouldUseMultiQuery(test.input); got != test.want {
				t.Fatalf("got=%t want=%t", got, test.want)
			}
		})
	}
}

func TestAuthoritativeCorrectionQueryDropsObsoleteService(t *testing.T) {
	query := AuthoritativeQuery(QueryPlanInput{
		UserMessage: "不是 payment，是 checkout，时间改成最近五分钟",
		Service:     "checkout",
		Intent:      intent.IntentResult{Intent: intent.IntentMetricsQuery, Service: "checkout"},
	})
	if !strings.Contains(query, "checkout") || strings.Contains(query, "payment") {
		t.Fatalf("query=%q", query)
	}
}

func queryTypeSet(queries []RAGSubQuery) map[QueryType]bool {
	result := map[QueryType]bool{}
	for _, query := range queries {
		result[query.Type] = true
	}
	return result
}
