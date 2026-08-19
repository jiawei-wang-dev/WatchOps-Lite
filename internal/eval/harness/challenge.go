package harness

import (
	"context"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

func evaluateChallenge(ctx context.Context, dataset ChallengeDataset) (ChallengeMetrics, []BadCase) {
	metrics := ChallengeMetrics{Cases: len(dataset.Cases)}
	if len(dataset.Cases) == 0 {
		return metrics, nil
	}
	now, _ := time.Parse(time.RFC3339, dataset.FixedNow)
	llm := &oracleRecognizer{delegate: intent.NewRuleBasedRecognizer()}
	recognizer := intent.NewHybridRecognizer(intent.Config{
		Enabled: true, LLMEnabled: true, Mode: "hybrid",
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, intent.NewRuleBasedRecognizer())
	intentHits, slotHits, slotTotal := 0, 0, 0
	tp, fp, fn, expectedClarify, expectedProceed := 0, 0, 0, 0, 0
	escalations, incorrectEscalations := 0, 0
	badCases := []BadCase{}
	for _, item := range dataset.Cases {
		focus := focusFromMap(item.InitialFocus)
		actual, _ := recognizer.Recognize(ctx, intent.RecognitionInput{Message: item.Message, Now: now, Focus: focus})
		decision := intent.ValidateSlots(item.Message, actual, nil, focus, 0.55)
		if string(actual.Intent) == item.ExpectedIntent {
			intentHits++
		} else {
			badCases = append(badCases, BadCase{Suite: "challenge", CaseID: item.ID, Stage: "intent", Expected: item.ExpectedIntent, Actual: actual.Intent, Reason: "challenge intent mismatch", Timestamp: now})
		}
		actualSlots := resultSlots(actual)
		caseSlotsOK := true
		for key, expected := range item.ExpectedSlots {
			slotTotal++
			if actualSlots[key] == expected {
				slotHits++
			} else {
				caseSlotsOK = false
				badCases = append(badCases, BadCase{Suite: "challenge", CaseID: item.ID, Stage: "slot", Expected: map[string]string{key: expected}, Actual: map[string]string{key: actualSlots[key]}, Reason: "challenge slot mismatch", Timestamp: now})
			}
		}
		_ = caseSlotsOK
		expectedIsClarify := item.ExpectedDecision == string(intent.DecisionClarify)
		actualIsClarify := decision.Decision == intent.DecisionClarify
		if expectedIsClarify {
			expectedClarify++
		} else {
			expectedProceed++
		}
		switch {
		case expectedIsClarify && actualIsClarify:
			tp++
		case !expectedIsClarify && actualIsClarify:
			fp++
		case expectedIsClarify && !actualIsClarify:
			fn++
		}
		if string(decision.Decision) != item.ExpectedDecision {
			badCases = append(badCases, BadCase{Suite: "challenge", CaseID: item.ID, Stage: "clarification", Expected: item.ExpectedDecision, Actual: decision.Decision, Reason: "challenge clarification decision mismatch", Timestamp: now})
		}
		attempted, _ := actual.Metadata["llm_attempted"].(bool)
		if attempted {
			escalations++
			if item.RuleSufficient {
				incorrectEscalations++
				badCases = append(badCases, BadCase{
					Suite: "challenge", CaseID: item.ID, Stage: "agent_execution",
					Expected: "rule_direct", Actual: "llm_escalated",
					Reason:    "rule-sufficient challenge case escalated unnecessarily",
					Timestamp: now,
				})
			}
		}
	}
	metrics.IntentAccuracy = ratio(intentHits, metrics.Cases)
	metrics.SlotCompleteness = ratio(slotHits, slotTotal)
	metrics.ClarificationPrecision = ratio(tp, tp+fp)
	metrics.ClarificationRecall = ratio(tp, tp+fn)
	metrics.OverClarificationRate = ratio(fp, expectedProceed)
	metrics.UnderClarificationRate = ratio(fn, expectedClarify)
	metrics.LLMEscalationRate = ratio(escalations, metrics.Cases)
	metrics.IncorrectLLMEscalationRate = ratio(incorrectEscalations, metrics.Cases)
	return metrics, badCases
}

func resultSlots(result intent.IntentResult) map[string]string {
	slots := map[string]string{"service": result.Service, "trace_id": result.TraceID, "symptom": result.Symptom}
	if result.TimeRange != nil {
		slots["time_range"] = result.TimeRange.Relative
	}
	return slots
}
