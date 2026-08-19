package intent

import (
	"context"
	"errors"
	"testing"
	"time"
)

type countingRecognizerStub struct {
	calls    int
	result   IntentResult
	err      error
	delegate Recognizer
}

func (s *countingRecognizerStub) Recognize(
	ctx context.Context,
	input RecognitionInput,
) (IntentResult, error) {
	s.calls++
	if s.delegate != nil {
		return s.delegate.Recognize(ctx, input)
	}
	return s.result, s.err
}

func TestHybridRuleFirstSkipsLLMForClearInput(t *testing.T) {
	rule := &countingRecognizerStub{delegate: NewRuleBasedRecognizer()}
	llm := &countingRecognizerStub{result: IntentResult{
		Intent: IntentLogsQuery, Confidence: 0.95, Source: "llm",
	}}
	result := recognizeForTest(t, Config{
		Enabled: true, Mode: "hybrid", LLMEnabled: true,
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, rule, RecognitionInput{
		Message: "查 payment 最近十分钟错误率",
		Now:     time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
	})
	if rule.calls != 1 || llm.calls != 0 ||
		result.Intent != IntentMetricsQuery ||
		result.Service != "payment" ||
		result.Symptom != "error" {
		t.Fatalf("rule=%d llm=%d result=%#v", rule.calls, llm.calls, result)
	}
	assertHybridMetadata(t, result, 0.78, false, 0, "", false, "rule")
}

func TestHybridRuleFirstSkipsLLMForClearGeneralRequests(t *testing.T) {
	for _, message := range []string{
		"你好，今天过得怎么样",
		"Can you explain what RAG means?",
		"写一首关于墨尔本下雨的俳句",
	} {
		rule := &countingRecognizerStub{delegate: NewRuleBasedRecognizer()}
		llm := &countingRecognizerStub{delegate: NewRuleBasedRecognizer()}
		result := recognizeForTest(t, Config{
			Enabled: true, Mode: "hybrid", LLMEnabled: true,
			MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
		}, llm, rule, RecognitionInput{Message: message})
		if result.Intent != IntentGeneralChat || llm.calls != 0 {
			t.Fatalf("message=%q llm=%d result=%#v", message, llm.calls, result)
		}
	}
}

func TestHybridRuleFirstDoesNotEscalateMissingSlots(t *testing.T) {
	rule := &countingRecognizerStub{delegate: NewRuleBasedRecognizer()}
	llm := &countingRecognizerStub{delegate: NewRuleBasedRecognizer()}
	input := RecognitionInput{Message: "查最近十分钟的错误率"}
	result := recognizeForTest(t, Config{
		Enabled: true, Mode: "hybrid", LLMEnabled: true,
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, rule, input)
	decision := ValidateSlots(input.Message, result, nil, FocusView{}, 0.55)
	if llm.calls != 0 || decision.Decision != DecisionClarify {
		t.Fatalf("llm=%d result=%#v decision=%#v", llm.calls, result, decision)
	}
}

func TestHybridRuleFirstSkipsLLMForResolvedMultiSignalRequests(t *testing.T) {
	for _, message := range []string{
		"不用查指标了，找一下 checkout runbook",
		"payment latency runbook，纯粹找文档",
		"inventory 报 too many connections，查日志和 runbook",
	} {
		rule := &countingRecognizerStub{delegate: NewRuleBasedRecognizer()}
		llm := &countingRecognizerStub{delegate: NewRuleBasedRecognizer()}
		result := recognizeForTest(t, Config{
			Enabled: true, Mode: "hybrid", LLMEnabled: true,
			MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
		}, llm, rule, RecognitionInput{Message: message})
		if llm.calls != 0 {
			t.Fatalf("message=%q llm=%d result=%#v", message, llm.calls, result)
		}
	}
}

func TestHybridEscalatesAmbiguousRuleToLLM(t *testing.T) {
	rule := &countingRecognizerStub{result: IntentResult{
		Intent:     IntentGeneralChat,
		Confidence: 0.4,
		Source:     "rule",
		Metadata:   map[string]any{"ambiguous": true},
	}}
	llm := &countingRecognizerStub{result: IntentResult{
		Intent:     IntentLogsQuery,
		Confidence: 0.91,
		Service:    "payment",
		Source:     "llm",
	}}
	result := recognizeForTest(t, Config{
		Enabled: true, Mode: "hybrid", LLMEnabled: true,
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, rule, RecognitionInput{Message: "帮我看看 payment"})
	if rule.calls != 1 || llm.calls != 1 ||
		result.Intent != IntentLogsQuery ||
		result.Service != "payment" ||
		result.Source != "llm" {
		t.Fatalf("rule=%d llm=%d result=%#v", rule.calls, llm.calls, result)
	}
	assertHybridMetadata(
		t,
		result,
		0.4,
		true,
		0.91,
		escalationRuleAmbiguous,
		false,
		"llm",
	)
}

func TestHybridEscalatesConflictingIntentSignals(t *testing.T) {
	rule := &countingRecognizerStub{result: IntentResult{
		Intent:     IntentMetricsQuery,
		Confidence: 0.85,
		Source:     "rule",
		Metadata:   map[string]any{"intent_signal_conflict": true},
	}}
	llm := &countingRecognizerStub{result: IntentResult{
		Intent:     IntentLogsQuery,
		Confidence: 0.88,
		Source:     "llm",
	}}
	result := recognizeForTest(t, Config{
		Enabled: true, Mode: "hybrid", LLMEnabled: true,
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, rule, RecognitionInput{Message: "查指标还是日志"})
	if rule.calls != 1 || llm.calls != 1 || result.Intent != IntentLogsQuery {
		t.Fatalf("rule=%d llm=%d result=%#v", rule.calls, llm.calls, result)
	}
	if result.Metadata["escalation_reason"] != escalationSignalConflict {
		t.Fatalf("metadata=%#v", result.Metadata)
	}
}

func TestHybridUsesLLMWhenRuleErrors(t *testing.T) {
	rule := &countingRecognizerStub{err: errors.New("rule unavailable")}
	llm := &countingRecognizerStub{result: IntentResult{
		Intent:     IntentKnowledgeQuery,
		Confidence: 0.9,
		Source:     "llm",
	}}
	result := recognizeForTest(t, Config{
		Enabled: true, Mode: "hybrid", LLMEnabled: true,
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, rule, RecognitionInput{Message: "找一下 runbook"})
	if rule.calls != 1 || llm.calls != 1 ||
		result.Intent != IntentKnowledgeQuery ||
		result.Metadata["escalation_reason"] != escalationRuleError {
		t.Fatalf("rule=%d llm=%d result=%#v", rule.calls, llm.calls, result)
	}
}

func TestHybridLLMFailureKeepsUsableRuleResult(t *testing.T) {
	for _, test := range []struct {
		name string
		llm  *countingRecognizerStub
	}{
		{
			name: "error",
			llm:  &countingRecognizerStub{err: errors.New("secret provider error")},
		},
		{
			name: "low confidence",
			llm: &countingRecognizerStub{result: IntentResult{
				Intent: IntentLogsQuery, Confidence: 0.2, Source: "llm",
			}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			rule := &countingRecognizerStub{result: IntentResult{
				Intent:     IntentMetricsQuery,
				Confidence: 0.6,
				Service:    "checkout",
				Source:     "rule",
			}}
			result := recognizeForTest(t, Config{
				Enabled: true, Mode: "hybrid", LLMEnabled: true,
				MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
			}, test.llm, rule, RecognitionInput{Message: "看看 checkout"})
			if rule.calls != 1 || test.llm.calls != 1 ||
				result.Intent != IntentMetricsQuery ||
				result.Service != "checkout" ||
				result.Source != "rule" ||
				result.Metadata["fallback_used"] != true ||
				!containsLimitation(result, "INTENT_LLM_FALLBACK") {
				t.Fatalf(
					"rule=%d llm=%d result=%#v",
					rule.calls,
					test.llm.calls,
					result,
				)
			}
			if _, leaked := result.Metadata["llm_failure_reason"]; leaked {
				t.Fatalf("metadata leaks provider error field: %#v", result.Metadata)
			}
		})
	}
}

func TestHybridReturnsSafeDefaultWhenBothRecognizersFail(t *testing.T) {
	rule := &countingRecognizerStub{err: errors.New("rule failed")}
	llm := &countingRecognizerStub{err: errors.New("llm failed")}
	result := recognizeForTest(t, Config{
		Enabled: true, Mode: "hybrid", LLMEnabled: true,
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, rule, RecognitionInput{Message: "hello"})
	if rule.calls != 1 || llm.calls != 1 ||
		result.Intent != IntentGeneralChat ||
		result.Source != "fallback" ||
		result.Metadata["fallback_used"] != true {
		t.Fatalf("rule=%d llm=%d result=%#v", rule.calls, llm.calls, result)
	}
}

func TestHybridDisabledLLMBehavesAsRuleOnly(t *testing.T) {
	rule := &countingRecognizerStub{result: IntentResult{
		Intent: IntentGeneralChat, Confidence: 0.4, Source: "rule",
	}}
	llm := &countingRecognizerStub{result: IntentResult{
		Intent: IntentLogsQuery, Confidence: 0.9, Source: "llm",
	}}
	result := recognizeForTest(t, Config{
		Enabled: true, Mode: "hybrid", LLMEnabled: false,
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, rule, RecognitionInput{Message: "hello"})
	if rule.calls != 1 || llm.calls != 0 ||
		result.Source != "rule" ||
		result.Metadata["llm_attempted"] != false ||
		result.Metadata["fallback_used"] != false {
		t.Fatalf("rule=%d llm=%d result=%#v", rule.calls, llm.calls, result)
	}
}

func TestHybridModeSemantics(t *testing.T) {
	tests := []struct {
		mode          string
		wantRuleCalls int
		wantLLMCalls  int
		wantIntent    IntentType
	}{
		{"rule", 1, 0, IntentMetricsQuery},
		{"llm", 0, 1, IntentLogsQuery},
		{"hybrid", 1, 0, IntentMetricsQuery},
	}
	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			rule := &countingRecognizerStub{result: IntentResult{
				Intent: IntentMetricsQuery, Confidence: 0.9, Source: "rule",
			}}
			llm := &countingRecognizerStub{result: IntentResult{
				Intent: IntentLogsQuery, Confidence: 0.9, Source: "llm",
			}}
			result := recognizeForTest(t, Config{
				Enabled: true, Mode: test.mode, LLMEnabled: true,
				MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
			}, llm, rule, RecognitionInput{Message: "clear input"})
			if rule.calls != test.wantRuleCalls ||
				llm.calls != test.wantLLMCalls ||
				result.Intent != test.wantIntent {
				t.Fatalf(
					"rule=%d llm=%d result=%#v",
					rule.calls,
					llm.calls,
					result,
				)
			}
		})
	}
}

func TestLLMModeFallsBackToRuleWhenLLMIsUnreliable(t *testing.T) {
	for _, llm := range []*countingRecognizerStub{
		{err: errors.New("llm unavailable")},
		{result: IntentResult{
			Intent: IntentLogsQuery, Confidence: 0.2, Source: "llm",
		}},
	} {
		rule := &countingRecognizerStub{result: IntentResult{
			Intent: IntentMetricsQuery, Confidence: 0.9, Source: "rule",
		}}
		result := recognizeForTest(t, Config{
			Enabled: true, Mode: "llm", LLMEnabled: true,
			MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
		}, llm, rule, RecognitionInput{Message: "show metrics"})
		if llm.calls != 1 || rule.calls != 1 ||
			result.Intent != IntentMetricsQuery ||
			result.Source != "rule" ||
			result.Metadata["fallback_used"] != true {
			t.Fatalf("llm=%d rule=%d result=%#v", llm.calls, rule.calls, result)
		}
	}
}

func TestHybridLLMResultPreservesDeterministicCurrentSlots(t *testing.T) {
	rule := &countingRecognizerStub{result: IntentResult{
		Intent:     IntentMetricsQuery,
		Confidence: 0.6,
		Service:    "payment",
		Symptom:    "error",
		TimeRange:  &TimeRangeHint{Relative: "last_10_minutes"},
		Source:     "rule",
	}}
	llm := &countingRecognizerStub{result: IntentResult{
		Intent:     IntentIncidentTriage,
		Confidence: 0.9,
		Service:    "checkout",
		Source:     "llm",
	}}
	result := recognizeForTest(t, Config{
		Enabled: true, Mode: "hybrid", LLMEnabled: true,
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, rule, RecognitionInput{
		Message: "查 payment 最近十分钟错误率",
		Now:     time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
	})
	if result.Intent != IntentIncidentTriage ||
		result.Service != "payment" ||
		result.Symptom != "error" ||
		result.TimeRange == nil ||
		result.TimeRange.Relative != "last_10_minutes" {
		t.Fatalf("result=%#v", result)
	}
}

func recognizeForTest(
	t *testing.T,
	config Config,
	llm Recognizer,
	rule Recognizer,
	input RecognitionInput,
) IntentResult {
	t.Helper()
	result, err := NewHybridRecognizer(config, llm, rule).
		Recognize(context.Background(), input)
	if err != nil {
		t.Fatalf("Recognize() error=%v", err)
	}
	return result
}

func assertHybridMetadata(
	t *testing.T,
	result IntentResult,
	ruleConfidence float64,
	llmAttempted bool,
	llmConfidence float64,
	escalationReason string,
	fallbackUsed bool,
	finalSource string,
) {
	t.Helper()
	if result.Metadata["rule_confidence"] != ruleConfidence ||
		result.Metadata["llm_attempted"] != llmAttempted ||
		result.Metadata["llm_confidence"] != llmConfidence ||
		result.Metadata["escalation_reason"] != escalationReason ||
		result.Metadata["fallback_used"] != fallbackUsed ||
		result.Metadata["final_source"] != finalSource {
		t.Fatalf("metadata=%#v", result.Metadata)
	}
}

func containsLimitation(result IntentResult, code string) bool {
	for _, limitation := range result.Limitations {
		if limitation.Code == code {
			return true
		}
	}
	return false
}
