package intent

import (
	"context"
	"testing"
	"time"
)

func TestRuleBasedRecognizesIncidentTriage(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "checkout-service returns 500 errors in the last 10 minutes",
		Now:     time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Intent != IntentIncidentTriage ||
		!hasTool(result, ToolQueryMetrics) ||
		!hasTool(result, ToolQueryLogs) ||
		!hasTool(result, ToolSearchKnowledge) ||
		result.Service != "checkout-service" ||
		result.TimeRange == nil ||
		result.TimeRange.Relative != "last_10_minutes" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuleBasedRecognizesTraceAnalysis(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "analyze trace 4bf92f3577b34da6a3ce929d0e0e4736",
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Intent != IntentTraceAnalysis ||
		result.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" ||
		!hasTool(result, ToolQueryTraces) {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuleBasedRecognizesKnowledgeQuery(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "find checkout runbook for payment timeout",
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Intent != IntentKnowledgeQuery ||
		!hasTool(result, ToolSearchKnowledge) ||
		!hasAgent(result, RoleKnowledge) ||
		!hasAgent(result, RoleSynthesis) ||
		!result.RAGHints.PreferRunbooks {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuleBasedTreatsRunbookPlusEvidenceSignalsAsIncident(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "Why did checkout error rate increase? Include metrics, logs, alerts, and runbook evidence.",
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Intent != IntentIncidentTriage ||
		!hasAgent(result, RoleEvidence) ||
		!hasAgent(result, RoleKnowledge) {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuleBasedRecognizesMetricsSignal(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "show checkout p95 latency metric",
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if (result.Intent != IntentMetricsQuery && result.Intent != IntentIncidentTriage) ||
		!hasTool(result, ToolQueryMetrics) {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuleBasedUnknownMessageUsesGeneralChat(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "hello there",
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Intent != IntentGeneralChat || result.Confidence == 0 {
		t.Fatalf("result = %#v", result)
	}
}

func hasTool(result IntentResult, tool ToolName) bool {
	for _, current := range result.SuggestedTools {
		if current == tool {
			return true
		}
	}
	return false
}

func hasAgent(result IntentResult, role AgentRole) bool {
	for _, current := range result.SuggestedAgents {
		if current == role {
			return true
		}
	}
	return false
}
