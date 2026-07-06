package intent

import (
	"context"
	"errors"
	"testing"
)

type recognizerStub struct {
	result IntentResult
	err    error
}

func (s recognizerStub) Recognize(context.Context, RecognitionInput) (IntentResult, error) {
	return s.result, s.err
}

func TestHybridRecognizerUsesLLMSuccess(t *testing.T) {
	recognizer := NewHybridRecognizer(
		Config{Enabled: true, Mode: "hybrid", LLMEnabled: true, MinLLMConfidence: 0.55},
		recognizerStub{result: IntentResult{
			Intent:     IntentLogsQuery,
			Confidence: 0.8,
			Source:     "llm",
		}},
		NewRuleBasedRecognizer(),
	)
	result, err := recognizer.Recognize(context.Background(), RecognitionInput{Message: "hello"})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Source != "llm" || result.Intent != IntentLogsQuery {
		t.Fatalf("result = %#v", result)
	}
}

func TestHybridRecognizerFallsBackWhenLLMErrors(t *testing.T) {
	recognizer := NewHybridRecognizer(
		Config{Enabled: true, Mode: "hybrid", LLMEnabled: true, MinLLMConfidence: 0.55},
		recognizerStub{err: errors.New("invalid json")},
		NewRuleBasedRecognizer(),
	)
	result, err := recognizer.Recognize(context.Background(), RecognitionInput{
		Message: "checkout 500 errors",
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Source != "rule" ||
		result.Intent != IntentIncidentTriage ||
		!containsLimitation(result, "INTENT_LLM_FALLBACK") ||
		result.Metadata["fallback_used"] != true {
		t.Fatalf("result = %#v", result)
	}
}

func TestHybridRecognizerFallsBackWhenLLMConfidenceIsLow(t *testing.T) {
	recognizer := NewHybridRecognizer(
		Config{Enabled: true, Mode: "hybrid", LLMEnabled: true, MinLLMConfidence: 0.55},
		recognizerStub{result: IntentResult{
			Intent:     IntentKnowledgeQuery,
			Confidence: 0.2,
			Source:     "llm",
		}},
		NewRuleBasedRecognizer(),
	)
	result, err := recognizer.Recognize(context.Background(), RecognitionInput{
		Message: "show checkout metrics",
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Source != "rule" || !containsLimitation(result, "INTENT_LLM_FALLBACK") {
		t.Fatalf("result = %#v", result)
	}
}

func TestHybridRecognizerReturnsSafeDefaultWhenAllRecognizersFail(t *testing.T) {
	recognizer := NewHybridRecognizer(
		Config{Enabled: true, Mode: "hybrid", LLMEnabled: true, MinLLMConfidence: 0.55},
		recognizerStub{err: errors.New("llm failed")},
		recognizerStub{err: errors.New("rule failed")},
	)
	result, err := recognizer.Recognize(context.Background(), RecognitionInput{Message: "hello"})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if result.Intent != IntentGeneralChat ||
		result.Source != "fallback" ||
		!containsLimitation(result, "INTENT_LLM_FALLBACK") {
		t.Fatalf("result = %#v", result)
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
