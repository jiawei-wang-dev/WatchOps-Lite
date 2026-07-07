package diagnosis

import (
	"context"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

func TestRuleBasedHypothesisGeneratorCreatesErrorIncidentHypotheses(t *testing.T) {
	set, err := NewRuleBasedHypothesisGenerator().Generate(context.Background(), GenerateInput{
		Intent: intent.IntentResult{
			Intent:     intent.IntentIncidentTriage,
			Confidence: 0.9,
		},
		Message: "checkout 5xx error rate increased",
		Symptom: "error",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !hasHypothesisTitle(set, "upstream dependency timeout") ||
		!hasHypothesisTitle(set, "database failure") ||
		!hasHypothesisTitle(set, "deployment regression") {
		t.Fatalf("hypotheses = %#v", set.Items)
	}
}

func TestRuleBasedHypothesisGeneratorCreatesLatencyHypotheses(t *testing.T) {
	set, err := NewRuleBasedHypothesisGenerator().Generate(context.Background(), GenerateInput{
		Intent: intent.IntentResult{
			Intent:     intent.IntentIncidentTriage,
			Confidence: 0.9,
		},
		Message: "checkout p95 latency is slow",
		Symptom: "latency",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !hasHypothesisTitle(set, "slow downstream dependency") ||
		!hasHypothesisTitle(set, "database bottleneck") ||
		!hasHypothesisTitle(set, "connection pool exhaustion") {
		t.Fatalf("hypotheses = %#v", set.Items)
	}
}

func hasHypothesisTitle(set HypothesisSet, title string) bool {
	for _, item := range set.Items {
		if item.Title == title {
			return true
		}
	}
	return false
}
