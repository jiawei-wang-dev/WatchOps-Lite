package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/turngovernance"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval/intenteval"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	sessionsummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
)

func Run(
	ctx context.Context,
	dataset Dataset,
	intentDataset intenteval.Dataset,
	options ...RunOptions,
) (Report, error) {
	fixedNow, err := timeFromDataset(dataset)
	if err != nil {
		return Report{}, err
	}
	intentReport := intenteval.Evaluate(ctx, intentDataset)
	report := Report{
		Version: dataset.Version, GeneratedAt: fixedNow,
		Mode: "offline_fixture", ExternalLLM: false,
		Intent: IntentMetrics{
			Cases:                 intentReport.Total,
			Accuracy:              intentReport.Metrics.IntentAccuracy,
			SlotFieldAccuracy:     intentReport.Metrics.SlotFieldAccuracy,
			JointExactMatch:       intentReport.Metrics.JointIntentSlotExactMatch,
			ClarificationAccuracy: intentReport.Metrics.ClarificationDecisionAccuracy,
		},
	}
	badCases := intentBadCases(intentReport, fixedNow)
	report.Retrieval, report.MultiQuery, badCases = appendRetrieval(ctx, dataset, badCases)

	executions := make(map[string]scenarioExecution, len(dataset.Scenarios))
	for _, scenario := range dataset.Scenarios {
		execution, executionErr := executeScenario(ctx, dataset, scenario)
		if executionErr != nil {
			badCases = append(badCases, BadCase{
				CaseID: scenario.CaseID, Stage: "agent_execution",
				Expected: "successful deterministic fixture execution",
				Actual:   executionErr.Error(), Reason: "scenario execution failed",
				Timestamp: fixedNow,
			})
			continue
		}
		executions[scenario.CaseID] = execution
		badCases = append(badCases, scenarioIntentBadCases(scenario, execution, fixedNow)...)
	}
	report.ToolEvidence, report.AgentCases, badCases = appendToolEvidence(
		ctx, dataset, executions, badCases,
	)
	report.AgentE2E = summarizeAgentCases(report.AgentCases)
	var runOptions RunOptions
	if len(options) > 0 {
		runOptions = options[0]
	}
	challengeMetrics, challengeBadCases := evaluateChallenge(ctx, runOptions.Challenge)
	report.Challenge = challengeMetrics
	badCases = append(badCases, challengeBadCases...)
	report.Benchmarks = Benchmarks{
		RuleFirstBefore: benchmarkIntent(ctx, intentDataset, "llm"),
		RuleFirstAfter:  benchmarkIntent(ctx, intentDataset, "hybrid"),
		Clarification:   benchmarkClarification(ctx, intentDataset),
		Context:         benchmarkContext(ctx),
		Retrieval:       append([]RetrievalMetrics{}, report.Retrieval...),
	}
	SortBadCases(badCases)
	report.BadCases = badCases
	report.BadCaseComparison = compareBadCases(runOptions.Baseline, badCases)
	report.Optimization = optimizationComparison(runOptions.OptimizationBaseline, report)
	report.FinalBadCaseComparison = compareFinalOptimizationBadCases(runOptions.FinalBadCaseBaseline, badCases)
	return report, nil
}

func compareFinalOptimizationBadCases(
	baseline []FinalOptimizationBadCaseBaseline,
	current []BadCase,
) FinalOptimizationBadCaseComparison {
	result := FinalOptimizationBadCaseComparison{Before: len(baseline), After: len(current)}
	currentByKey := map[string]BadCase{}
	for _, item := range current {
		currentByKey[item.CaseID+"\x00"+item.Stage] = item
	}
	for _, item := range baseline {
		caseResult := FinalOptimizationBadCaseResult{FinalOptimizationBadCaseBaseline: item}
		if actual, ok := currentByKey[item.CaseID+"\x00"+item.Stage]; ok {
			caseResult.AfterBehavior = actual.Reason
		} else {
			caseResult.AfterBehavior = "passed current evaluation"
			caseResult.Fixed = true
			result.Fixed++
		}
		result.Cases = append(result.Cases, caseResult)
	}
	return result
}

func appendRetrieval(
	ctx context.Context,
	dataset Dataset,
	badCases []BadCase,
) ([]RetrievalMetrics, MultiQueryMetrics, []BadCase) {
	metrics, multiQuery, retrievalBadCases := evaluateRetrieval(ctx, dataset)
	return metrics, multiQuery, append(badCases, retrievalBadCases...)
}

func optimizationComparison(before OptimizationBaseline, report Report) OptimizationComparison {
	retrievalBadCases := 0
	for _, item := range report.BadCases {
		if item.Suite != "challenge" && item.Stage == "retrieval" {
			retrievalBadCases++
		}
	}
	after := OptimizationBaseline{
		CapturedAt: report.GeneratedAt.Format(time.RFC3339),
		Intent: OptimizationIntentSnapshot{
			LLMEscalationRate:         report.Challenge.LLMEscalationRate,
			IncorrectEscalationRate:   report.Challenge.IncorrectLLMEscalationRate,
			ChallengeBehaviorAccuracy: report.Challenge.IntentAccuracy,
			FixedRegressionAccuracy:   report.Intent.Accuracy,
		},
		Retrieval: OptimizationRetrievalSnapshot{
			SingleQueryRecallAt5: report.MultiQuery.SingleQueryRecallAt5,
			SingleQueryMRR:       report.MultiQuery.SingleQueryMRR,
			MultiQueryRecallAt5:  report.MultiQuery.MultiQueryRecallAt5,
			MultiQueryMRR:        report.MultiQuery.MultiQueryMRR,
			BadCases:             retrievalBadCases,
		},
		AgentE2E: OptimizationAgentSnapshot{
			TaskSuccessRate:  report.AgentE2E.TaskSuccessRate,
			DecisionAccuracy: report.AgentE2E.DecisionAccuracy,
			UnsafeActionRate: report.AgentE2E.UnsafeActionRate,
			Timeout:          report.AgentE2E.Timeout,
			RepeatedStops:    report.AgentE2E.RepeatedToolCallStops,
		},
	}
	return OptimizationComparison{Before: before, After: after}
}

func appendToolEvidence(
	ctx context.Context,
	dataset Dataset,
	executions map[string]scenarioExecution,
	badCases []BadCase,
) (ToolEvidenceMetrics, []AgentCaseResult, []BadCase) {
	metrics, cases, toolBadCases := evaluateToolEvidence(ctx, dataset, executions)
	return metrics, cases, append(badCases, toolBadCases...)
}

func timeFromDataset(dataset Dataset) (time.Time, error) {
	value, err := time.Parse(time.RFC3339, dataset.FixedNow)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse dataset fixed_now: %w", err)
	}
	return value.UTC(), nil
}

func intentBadCases(report intenteval.Report, timestamp time.Time) []BadCase {
	result := []BadCase{}
	for _, item := range report.Cases {
		if item.Passed {
			continue
		}
		stage := "slot"
		expected, actual := any(item.ExpectedSlots), any(item.ActualSlots)
		if item.ExpectedIntent != item.ActualIntent {
			stage, expected, actual = "intent", item.ExpectedIntent, item.ActualIntent
		} else if item.ExpectedDecision != item.ActualDecision {
			stage, expected, actual = "clarification", item.ExpectedDecision, item.ActualDecision
		}
		result = append(result, BadCase{
			CaseID: item.ID, Stage: stage, Expected: expected, Actual: actual,
			Reason: item.FailureReason, Timestamp: timestamp,
		})
	}
	return result
}

func scenarioIntentBadCases(
	scenario Scenario,
	execution scenarioExecution,
	timestamp time.Time,
) []BadCase {
	result := []BadCase{}
	if actual := string(execution.intentResult.Intent); actual != scenario.ExpectedIntent {
		result = append(result, BadCase{
			CaseID: scenario.CaseID, Stage: "intent", Expected: scenario.ExpectedIntent,
			Actual: actual, Reason: "scenario intent mismatch", Timestamp: timestamp,
		})
	}
	actualSlots := map[string]string{
		"service":  execution.intentResult.Service,
		"trace_id": execution.intentResult.TraceID,
		"symptom":  execution.intentResult.Symptom,
	}
	if execution.intentResult.TimeRange != nil {
		actualSlots["time_range"] = execution.intentResult.TimeRange.Relative
	}
	for key, expected := range scenario.ExpectedSlots {
		if actualSlots[key] == expected {
			continue
		}
		result = append(result, BadCase{
			CaseID: scenario.CaseID, Stage: "slot", Expected: map[string]string{key: expected},
			Actual: map[string]string{key: actualSlots[key]}, Reason: "scenario slot mismatch",
			Timestamp: timestamp,
		})
	}
	if actual := string(execution.decision.Decision); actual != scenario.ExpectedDecision {
		result = append(result, BadCase{
			CaseID: scenario.CaseID, Stage: "clarification", Expected: scenario.ExpectedDecision,
			Actual: actual, Reason: "scenario clarification decision mismatch", Timestamp: timestamp,
		})
	}
	return result
}

func summarizeAgentCases(cases []AgentCaseResult) AgentMetrics {
	result := AgentMetrics{Cases: len(cases)}
	latencies := make([]float64, 0, len(cases))
	toolCalls := 0
	for _, item := range cases {
		switch item.ObservedStatus {
		case "completed":
			result.Completed++
		case "clarified":
			result.Clarified++
		case "timeout":
			result.Timeout++
		}
		if !item.TaskSuccess {
			result.Failed++
		}
		if item.DecisionCorrect {
			result.DecisionAccuracy++
		}
		result.RequiredEvidenceCoverage += item.RequiredEvidenceCoverage
		if item.UnsafeAction {
			result.UnsafeActionRate++
		}
		if item.RepeatedToolCallStop {
			result.RepeatedToolCallStops++
		}
		toolCalls += len(item.ActualTools)
		latencies = append(latencies, item.LatencyMS)
	}
	result.AverageToolCalls = ratioFloat(toolCalls, len(cases))
	successes := len(cases) - result.Failed
	result.TaskSuccessRate = ratio(successes, len(cases))
	result.DecisionAccuracy /= float64(maxInt(1, len(cases)))
	result.RequiredEvidenceCoverage /= float64(maxInt(1, len(cases)))
	result.UnsafeActionRate /= float64(maxInt(1, len(cases)))
	result.FailureRate = ratio(result.Failed, len(cases))
	result.TimeoutRate = ratio(result.Timeout, len(cases))
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		result.P50LatencyMS = percentile(latencies, 0.50)
		result.P95LatencyMS = percentile(latencies, 0.95)
	}
	return result
}

func compareBadCases(baseline []BadCaseBaseline, current []BadCase) BadCaseComparison {
	result := BadCaseComparison{BeforeRecords: len(baseline)}
	beforeIDs, currentIDs := map[string]struct{}{}, map[string]BadCase{}
	for _, item := range baseline {
		beforeIDs[item.CaseID] = struct{}{}
	}
	for _, item := range current {
		if item.Suite == "challenge" {
			result.ChallengeFindings++
			continue
		}
		key := item.CaseID + "\x00" + item.Stage
		currentIDs[key] = item
	}
	result.BeforeUnique = len(beforeIDs)
	afterUnique := map[string]struct{}{}
	for _, item := range currentIDs {
		result.AfterRecords++
		afterUnique[item.CaseID] = struct{}{}
	}
	result.AfterUnique = len(afterUnique)
	baselineKeys := map[string]struct{}{}
	for _, item := range baseline {
		key := item.CaseID + "\x00" + item.Stage
		baselineKeys[key] = struct{}{}
		fix := BadCaseFix{CaseID: item.CaseID, Stage: item.Stage, RootCause: item.RootCause, FixType: item.FixType, BeforeBehavior: item.BeforeBehavior}
		if actual, ok := currentIDs[key]; ok {
			fix.AfterBehavior = actual.Reason
			result.StillFailing++
		} else {
			fix.AfterBehavior = "passed behavior-level evaluation"
			fix.Fixed = true
			result.Fixed++
		}
		result.Cases = append(result.Cases, fix)
	}
	for key := range currentIDs {
		if _, ok := baselineKeys[key]; !ok {
			result.NewRegression++
		}
	}
	return result
}

type oracleRecognizer struct {
	calls    int
	delegate intent.Recognizer
}

func (r *oracleRecognizer) Recognize(ctx context.Context, input intent.RecognitionInput) (intent.IntentResult, error) {
	r.calls++
	result, err := r.delegate.Recognize(ctx, input)
	if err == nil {
		result.Source = "fixture_llm"
		result.Confidence = maxFloat(result.Confidence, 0.9)
	}
	return result, err
}

func benchmarkIntent(ctx context.Context, dataset intenteval.Dataset, mode string) RuleFirstBenchmark {
	now, _ := time.Parse(time.RFC3339, dataset.FixedNow)
	llm := &oracleRecognizer{delegate: intent.NewRuleBasedRecognizer()}
	recognizer := intent.NewHybridRecognizer(intent.Config{
		Enabled: true, LLMEnabled: true, Mode: mode,
		MinRuleConfidence: 0.75, MinLLMConfidence: 0.55,
	}, llm, intent.NewRuleBasedRecognizer())
	result := RuleFirstBenchmark{Mode: mode, Cases: len(dataset.Cases)}
	for _, item := range dataset.Cases {
		actual, _ := recognizer.Recognize(ctx, intent.RecognitionInput{Message: item.Message, Now: now})
		if string(actual.Intent) == item.ExpectedIntent {
			result.IntentAccuracy += 1
		}
		attempted, _ := actual.Metadata["llm_attempted"].(bool)
		if attempted {
			result.LLMEscalationCount++
		} else {
			result.RuleDirectCount++
		}
		fallback, _ := actual.Metadata["fallback_used"].(bool)
		if fallback {
			result.FallbackCount++
		}
	}
	result.IntentAccuracy /= float64(maxInt(1, result.Cases))
	result.LLMCallRate = ratio(llm.calls, result.Cases)
	return result
}

func benchmarkClarification(ctx context.Context, dataset intenteval.Dataset) ClarificationBenchmark {
	now, _ := time.Parse(time.RFC3339, dataset.FixedNow)
	result := ClarificationBenchmark{}
	for _, item := range dataset.Cases {
		if item.ExpectedDecision != string(intent.DecisionClarify) {
			continue
		}
		result.Cases++
		recognized, _ := intent.NewRuleBasedRecognizer().Recognize(ctx, intent.RecognitionInput{Message: item.Message, Now: now})
		decision := intent.ValidateSlots(item.Message, recognized, nil, intent.FocusView{}, 0.55)
		if decision.Decision != intent.DecisionClarify {
			result.RAGCalls++
			result.AgentCalls++
			result.ToolCalls++
		}
	}
	result.AvoidedRAGCalls = result.Cases - result.RAGCalls
	result.AvoidedAgentCalls = result.Cases - result.AgentCalls
	result.AvoidedToolCalls = result.Cases - result.ToolCalls
	return result
}

func benchmarkContext(ctx context.Context) ContextBenchmark {
	messages := make([]session.Message, 0, 18)
	for index := 0; index < 18; index++ {
		content := fmt.Sprintf("turn %02d checkout payment investigation last_20_minutes", index)
		if index == 2 {
			content += " api_key=should-be-redacted"
		}
		if index == 4 {
			content += " trace_id=4bf92f3577b34da6a3ce929d0e0e4736"
		}
		messages = append(messages, session.Message{Role: session.RoleUser, Content: content})
	}
	rawText := messageText(messages)
	summary, _ := sessionsummary.NewDeterministic().Summarize(ctx, session.EmptySummary(), messages[:12])
	focus := turngovernance.BoundFocus(session.SessionFocus{
		Version: 1, CurrentService: "checkout", CurrentTimeRange: "last_20_minutes",
		CurrentTraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		KnownSlots:     map[string]string{"service": "checkout", "time_range": "last_20_minutes", "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"},
		RecentMessages: messages, Summary: summary.Content,
	})
	governedText := focus.Summary + "\n" + messageText(focus.RecentMessages)
	focusEncoded, _ := json.Marshal(focus)
	return ContextBenchmark{
		RawHistory: ContextMeasurement{
			MessageCount: len(messages), ContextChars: utf8.RuneCountInString(rawText),
			ContextBytes: len(rawText), RetainedImportantFields: []string{"service", "time_range", "trace_id"},
			RedactedFields: []string{}, FocusSizeBytes: 0,
		},
		Governed: ContextMeasurement{
			MessageCount: len(focus.RecentMessages), ContextChars: utf8.RuneCountInString(governedText),
			ContextBytes: len(governedText), RetainedImportantFields: retainedFocusFields(focus),
			RedactedFields: detectedRedactions(rawText, governedText), FocusSizeBytes: len(focusEncoded),
		},
	}
}

func messageText(messages []session.Message) string {
	parts := make([]string, 0, len(messages))
	for _, item := range messages {
		parts = append(parts, item.Content)
	}
	return strings.Join(parts, "\n")
}

func retainedFocusFields(focus session.SessionFocus) []string {
	result := []string{}
	for _, key := range []string{"service", "time_range", "trace_id"} {
		if strings.TrimSpace(focus.KnownSlots[key]) != "" {
			result = append(result, key)
		}
	}
	return result
}

func detectedRedactions(raw, governed string) []string {
	if strings.Contains(raw, "api_key=") && !strings.Contains(governed, "should-be-redacted") {
		return []string{"api_key"}
	}
	return []string{}
}

func ratioFloat(numerator, denominator int) float64 { return ratio(numerator, denominator) }

func percentile(values []float64, value float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1)*value + 0.5)
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
