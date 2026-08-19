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

func TestRuleBasedRecognizesCheckoutRecentErrorRate(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "查看 checkout 最近 10 分钟错误率",
		Now:     time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Service != "checkout" ||
		(result.Intent != IntentMetricsQuery && result.Intent != IntentIncidentTriage) ||
		result.TimeRange == nil || result.TimeRange.Relative != "last_10_minutes" ||
		result.Symptom != "error" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuleBasedIntentEvalRegressions(t *testing.T) {
	tests := []struct {
		name    string
		message string
		intent  IntentType
		service string
		symptom string
	}{
		{
			name:    "Chinese error rate symptom",
			message: "查 payment 最近十分钟的错误率",
			intent:  IntentMetricsQuery,
			service: "payment",
			symptom: "error",
		},
		{
			name:    "composite diagnostic request",
			message: "查 payment 错误率、看看 Trace、再给建议",
			intent:  IntentIncidentTriage,
			service: "payment",
			symptom: "error",
		},
		{
			name:    "explicit log preference",
			message: "payment exception 日志还是指标？先看日志",
			intent:  IntentLogsQuery,
			service: "payment",
			symptom: "exception",
		},
	}

	recognizer := NewRuleBasedRecognizer()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := recognizer.Recognize(context.Background(), RecognitionInput{
				Message: test.message,
				Now:     time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("Recognize() error = %v", err)
			}
			if result.Intent != test.intent ||
				result.Service != test.service ||
				result.Symptom != test.symptom {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestRuleBasedMarksStructuredEscalationSignals(t *testing.T) {
	recognizer := NewRuleBasedRecognizer()
	conflict, err := recognizer.Recognize(context.Background(), RecognitionInput{
		Message: "查 payment 指标还是日志",
	})
	if err != nil {
		t.Fatalf("Recognize(conflict) error=%v", err)
	}
	if conflict.Metadata["intent_signal_conflict"] != true {
		t.Fatalf("conflict metadata=%#v", conflict.Metadata)
	}

	explicit, err := recognizer.Recognize(context.Background(), RecognitionInput{
		Message: "payment 指标还是日志？先看日志",
	})
	if err != nil {
		t.Fatalf("Recognize(explicit) error=%v", err)
	}
	if explicit.Intent != IntentLogsQuery ||
		explicit.Metadata["intent_signal_conflict"] != false {
		t.Fatalf("explicit result=%#v", explicit)
	}

	unresolved, err := recognizer.Recognize(context.Background(), RecognitionInput{
		Message: "第二个",
		Focus: FocusView{
			Available:  true,
			LastIntent: string(IntentIncidentTriage),
			KnownSlots: map[string]string{},
			Candidates: []string{},
		},
	})
	if err != nil {
		t.Fatalf("Recognize(unresolved) error=%v", err)
	}
	if unresolved.Metadata["context_reference_unresolved"] != true ||
		unresolved.Metadata["ambiguous"] != true {
		t.Fatalf("unresolved metadata=%#v", unresolved.Metadata)
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

func TestRuleBasedPrioritizesCompoundIncidentOverTraceKeyword(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "分析 checkout 是否被 payment 延迟拖慢，结合 Trace 和 runbook",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != IntentIncidentTriage || result.Service != "checkout" || result.Symptom != "latency" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuleBasedTreatsDatabaseConnectionErrorsAsIncident(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "排查 orders 数据库 connection refused，查日志和处理手册",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != IntentIncidentTriage || result.Symptom != "error" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuleBasedCarriesFocusIntoExplicitFollowupIntent(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "那它的日志呢？",
		Focus: FocusView{
			Available: true, LastIntent: string(IntentMetricsQuery),
			KnownSlots: map[string]string{"service": "payment", "time_range": "last_10_minutes"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != IntentLogsQuery || result.Service != "payment" ||
		result.TimeRange == nil || result.TimeRange.Relative != "last_10_minutes" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuleBasedUsesCorrectedServiceRatherThanNegatedService(t *testing.T) {
	result, err := NewRuleBasedRecognizer().Recognize(context.Background(), RecognitionInput{
		Message: "别查 checkout 了，改查 payment 最近 5 分钟错误日志",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Service != "payment" || result.Intent != IntentLogsQuery {
		t.Fatalf("result = %#v", result)
	}
}
