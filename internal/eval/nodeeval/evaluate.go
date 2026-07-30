package nodeeval

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/multiagent"
)

func Evaluate(ctx context.Context, dataset Dataset) Report {
	started := time.Now()
	now, _ := time.Parse(time.RFC3339, dataset.FixedNow)
	report := Report{
		DatasetVersion: dataset.Version,
		GeneratedAt:    time.Now().UTC(),
		Configuration: map[string]any{
			"mode":      "local_deterministic",
			"fixed_now": dataset.FixedNow,
			"network":   false,
		},
		RealLLMUsed: false,
		Stages:      map[EvalStage]StageReport{},
	}
	report.Stages[EvalStageIntent] = evaluateIntent(ctx, dataset.Intent, now)
	report.Stages[EvalStageSlot] = evaluateSlot(ctx, dataset.Slot, now)
	report.Stages[EvalStageContext] = evaluateContext(ctx, dataset.Context, now)
	report.Stages[EvalStageRouting] = evaluateRouting(dataset.Routing)
	report.Stages[EvalStageRetrieval] = evaluateRetrieval(
		dataset.Retrieval,
		dataset.RetrievalCorpus,
	)
	report.Stages[EvalStageFallback] = evaluateFallback(dataset.Fallback)
	declaredTotal, matched, verifiedTotal, verifiedPassed := 0, 0, 0, 0
	for _, stage := range report.Stages {
		declaredTotal += stage.Total
		matched += stage.Passed
		verifiedTotal += stage.Verified
		for _, current := range stage.Cases {
			if current.Verification == "executed" && current.Passed {
				verifiedPassed++
			}
		}
	}
	report.DurationMS = time.Since(started).Milliseconds()
	report.Summary = map[string]any{
		"total":                    verifiedTotal,
		"passed":                   verifiedPassed,
		"failed":                   verifiedTotal - verifiedPassed,
		"pass_rate":                ratio(verifiedPassed, verifiedTotal),
		"declared_case_total":      declaredTotal,
		"expectations_matched":     matched,
		"contract_only_case_count": declaredTotal - verifiedTotal,
	}
	return report
}

func evaluateIntent(
	ctx context.Context,
	cases []IntentCase,
	now time.Time,
) StageReport {
	stage := newStage(EvalStageIntent, len(cases))
	confusion := map[string]map[string]int{}
	serviceMatches, symptomMatches, traceMatches := 0, 0, 0
	var latency float64
	recognizer := intent.NewRuleBasedRecognizer()
	for _, current := range cases {
		started := time.Now()
		result, err := recognizer.Recognize(ctx, intent.RecognitionInput{
			Message: current.Message, Now: now, Focus: focusView(current.Focus),
		})
		elapsed := durationMS(started)
		latency += elapsed
		actualIntent := string(result.Intent)
		if confusion[current.ExpectedIntent] == nil {
			confusion[current.ExpectedIntent] = map[string]int{}
		}
		confusion[current.ExpectedIntent][actualIntent]++
		serviceOK := result.Service == current.ExpectedService
		symptomOK := result.Symptom == current.ExpectedSymptom
		traceOK := result.TraceID == current.ExpectedTraceID
		if serviceOK {
			serviceMatches++
		}
		if symptomOK {
			symptomMatches++
		}
		if traceOK {
			traceMatches++
		}
		passed := err == nil && actualIntent == current.ExpectedIntent &&
			serviceOK && symptomOK && traceOK &&
			result.Confidence >= current.ExpectedConfidenceMin &&
			(current.ExpectedSource == "" || result.Source == current.ExpectedSource)
		reason := ""
		if !passed {
			reason = "intent or extracted fields did not match"
		}
		stage.add(CaseResult{
			ID: current.ID, Passed: passed, FailureReason: reason,
			LatencyMS: elapsed,
			Actual: map[string]any{
				"intent": result.Intent, "service": result.Service,
				"symptom": result.Symptom, "trace_id": result.TraceID,
				"confidence": result.Confidence, "source": result.Source,
			},
			Verification: "executed",
		})
	}
	stage.Metrics["accuracy"] = ratio(stage.Passed, stage.Total)
	stage.Metrics["confusion_matrix"] = confusion
	stage.Metrics["service_exact_match"] = ratio(serviceMatches, stage.Total)
	stage.Metrics["symptom_exact_match"] = ratio(symptomMatches, stage.Total)
	stage.Metrics["trace_id_exact_match"] = ratio(traceMatches, stage.Total)
	stage.Metrics["average_latency_ms"] = average(latency, stage.Total)
	return stage
}

func evaluateSlot(
	ctx context.Context,
	cases []SlotCase,
	now time.Time,
) StageReport {
	stage := newStage(EvalStageSlot, len(cases))
	recognizer := intent.NewRuleBasedRecognizer()
	trueClarify, predictedClarify, correctClarify := 0, 0, 0
	slotMatches, missingMatches, unsafeProceed, falseClarify := 0, 0, 0, 0
	for _, current := range cases {
		started := time.Now()
		result, _ := recognizer.Recognize(ctx, intent.RecognitionInput{
			Message: current.Message, Now: now, Focus: focusView(current.Focus),
		})
		if current.Intent != "" {
			result.Intent = intent.IntentType(current.Intent)
			result = intent.Normalize(result)
		}
		decision := intent.ValidateSlots(
			current.Message, result, nil, focusView(current.Focus), 0.55,
		)
		expectedClarify := current.ExpectedDecision == string(intent.DecisionClarify)
		actualClarify := decision.Decision == intent.DecisionClarify
		if expectedClarify {
			trueClarify++
		}
		if actualClarify {
			predictedClarify++
		}
		if expectedClarify && actualClarify {
			correctClarify++
		}
		if expectedClarify && !actualClarify {
			unsafeProceed++
		}
		if !expectedClarify && actualClarify {
			falseClarify++
		}
		slotsOK := mapContains(decision.KnownSlots, current.ExpectedKnownSlots)
		missingOK := sameStrings(decision.MissingRequired, current.ExpectedMissingRequired)
		if slotsOK {
			slotMatches++
		}
		if missingOK {
			missingMatches++
		}
		passed := string(decision.Decision) == current.ExpectedDecision &&
			slotsOK && missingOK &&
			(current.ExpectedReasonCode == "" ||
				decision.ReasonCode == current.ExpectedReasonCode) &&
			(current.ExpectedQuestionContains == "" ||
				strings.Contains(decision.ClarifyQuestion, current.ExpectedQuestionContains)) &&
			(decision.Decision != intent.DecisionProceed) ==
				current.ExpectedNoToolExecution
		reason := ""
		if !passed {
			reason = "slot decision, known slots, or missing slots did not match"
		}
		stage.add(CaseResult{
			ID: current.ID, Passed: passed, FailureReason: reason,
			LatencyMS: durationMS(started),
			Actual: map[string]any{
				"decision": decision.Decision, "known_slots": decision.KnownSlots,
				"missing_required": decision.MissingRequired,
				"reason_code":      decision.ReasonCode,
				"question":         decision.ClarifyQuestion,
				"tool_execution":   decision.Decision == intent.DecisionProceed,
			},
			Verification: "executed",
		})
	}
	stage.Metrics["decision_accuracy"] = ratio(stage.Passed, stage.Total)
	stage.Metrics["clarification_precision"] = ratio(correctClarify, predictedClarify)
	stage.Metrics["clarification_recall"] = ratio(correctClarify, trueClarify)
	stage.Metrics["missing_slot_exact_match"] = ratio(missingMatches, stage.Total)
	stage.Metrics["slot_accuracy"] = ratio(slotMatches, stage.Total)
	stage.Metrics["false_clarification_rate"] = ratio(falseClarify, stage.Total)
	stage.Metrics["unsafe_proceed_rate"] = ratio(unsafeProceed, stage.Total)
	return stage
}

func evaluateContext(
	ctx context.Context,
	cases []ContextCase,
	now time.Time,
) StageReport {
	stage := newStage(EvalStageContext, len(cases))
	recognizer := intent.NewRuleBasedRecognizer()
	referenceTotal, referencePassed := 0, 0
	carryTotal, carryPassed, overrideTotal, overridePassed := 0, 0, 0, 0
	recoveryTotal, recoveryPassed := 0, 0
	for _, current := range cases {
		started := time.Now()
		focus := Focus{KnownSlots: map[string]string{}}
		passed := true
		turnActual := []map[string]any{}
		previousClarify := false
		for _, turn := range current.Turns {
			turnPassed := true
			view := focusView(&focus)
			result, _ := recognizer.Recognize(ctx, intent.RecognitionInput{
				Message: turn.Message, Now: now, Focus: view,
			})
			decision := intent.ValidateSlots(turn.Message, result, nil, view, 0.55)
			status := string(decision.Decision)
			if turn.ExpectedStatus != "" && status != turn.ExpectedStatus {
				turnPassed = false
			}
			if turn.ExpectedIntent != "" && string(decision.Result.Intent) != turn.ExpectedIntent {
				turnPassed = false
			}
			if !mapContains(decision.KnownSlots, turn.ExpectedSlots) {
				turnPassed = false
			}
			if !turnPassed {
				passed = false
			}
			if strings.Contains(turn.Message, "第二个") ||
				strings.Contains(turn.Message, "刚才") {
				referenceTotal++
				if turnPassed {
					referencePassed++
				}
			}
			for key, expected := range turn.ExpectedSlots {
				if old := focus.KnownSlots[key]; old != "" {
					if old == expected {
						carryTotal++
						if decision.KnownSlots[key] == expected {
							carryPassed++
						}
					} else {
						overrideTotal++
						if decision.KnownSlots[key] == expected {
							overridePassed++
						}
					}
				}
			}
			if previousClarify {
				recoveryTotal++
				if decision.Decision == intent.DecisionProceed && turnPassed {
					recoveryPassed++
				}
			}
			previousClarify = decision.Decision == intent.DecisionClarify
			focus.LastIntent = string(decision.Result.Intent)
			focus.KnownSlots = cloneSlots(decision.KnownSlots)
			focus.PendingQuestion = decision.ClarifyQuestion
			focus.TurnStatus = status
			focus.Candidates = serviceCandidates(turn.Message, focus.Candidates)
			turnActual = append(turnActual, map[string]any{
				"intent": decision.Result.Intent, "status": status,
				"slots": decision.KnownSlots,
			})
		}
		reason := ""
		if !passed {
			reason = "one or more turn expectations did not match"
		}
		stage.add(CaseResult{
			ID: current.ID, Passed: passed, FailureReason: reason,
			LatencyMS: durationMS(started),
			Actual: map[string]any{
				"session_id": "nodeeval:" + current.ID,
				"turns":      turnActual,
			},
			Verification: "executed",
		})
	}
	stage.Metrics["multi_turn_success_rate"] = ratio(stage.Passed, stage.Total)
	stage.Metrics["reference_resolution_accuracy"] = ratio(referencePassed, referenceTotal)
	stage.Metrics["slot_carry_over_accuracy"] = ratio(carryPassed, carryTotal)
	stage.Metrics["current_turn_override_accuracy"] = ratio(overridePassed, overrideTotal)
	stage.Metrics["clarification_recovery_rate"] = ratio(recoveryPassed, recoveryTotal)
	stage.Metrics["isolated_session_count"] = len(cases)
	return stage
}

func evaluateRouting(cases []RoutingCase) StageReport {
	stage := newStage(EvalStageRouting, len(cases))
	roleTP, roleFP, roleFN := map[string]int{}, map[string]int{}, map[string]int{}
	unnecessary, missing := 0, 0
	for _, current := range cases {
		started := time.Now()
		plan := multiagent.PlanAgents(intent.IntentResult{
			Intent:     intent.IntentType(current.Intent),
			Confidence: current.Confidence,
			Source:     "node_eval",
		})
		actual := roleStrings(plan.SelectedAgents)
		actualSkipped := roleStrings(plan.SkippedAgents)
		expected := append([]string{}, current.ExpectedSelectedAgents...)
		exact := sameStrings(actual, expected) &&
			(len(current.ExpectedSkippedAgents) == 0 ||
				sameStrings(actualSkipped, current.ExpectedSkippedAgents)) &&
			plan.DynamicRoutingEnabled == current.ExpectedDynamicRoutingEnabled
		expectedSet, actualSet := stringSet(expected), stringSet(actual)
		for _, role := range []string{"triage", "evidence", "knowledge", "synthesis"} {
			_, want := expectedSet[role]
			_, got := actualSet[role]
			switch {
			case want && got:
				roleTP[role]++
			case !want && got:
				roleFP[role]++
				unnecessary++
			case want && !got:
				roleFN[role]++
				missing++
			}
		}
		reason := ""
		if !exact {
			reason = "selected roles or dynamic-routing flag did not match"
		}
		stage.add(CaseResult{
			ID: current.ID, Passed: exact, FailureReason: reason,
			LatencyMS: durationMS(started),
			Actual: map[string]any{
				"selected_agents":         actual,
				"skipped_agents":          actualSkipped,
				"dynamic_routing_enabled": plan.DynamicRoutingEnabled,
			},
			Verification: "executed",
		})
	}
	precision, recall := map[string]float64{}, map[string]float64{}
	for _, role := range []string{"triage", "evidence", "knowledge", "synthesis"} {
		precision[role] = ratio(roleTP[role], roleTP[role]+roleFP[role])
		recall[role] = ratio(roleTP[role], roleTP[role]+roleFN[role])
	}
	stage.Metrics["exact_match"] = ratio(stage.Passed, stage.Total)
	stage.Metrics["per_role_precision"] = precision
	stage.Metrics["per_role_recall"] = recall
	stage.Metrics["unnecessary_role_rate"] = ratio(unnecessary, stage.Total*4)
	stage.Metrics["missing_required_role_rate"] = ratio(missing, stage.Total*4)
	return stage
}

func evaluateRetrieval(
	cases []RetrievalCase,
	corpus []RetrievalDocument,
) StageReport {
	stage := newStage(EvalStageRetrieval, len(cases))
	hits, relevantFound, relevantTotal, empty := 0, 0, 0, 0
	relevantCaseTotal := 0
	var reciprocalRank, ndcg, latency float64
	latencies := make([]float64, 0, len(cases))
	for _, current := range cases {
		started := time.Now()
		ranked := rankRetrievalCorpus(current.Query, corpus, 5)
		elapsed := durationMS(started)
		latency += elapsed
		latencies = append(latencies, elapsed)
		relevant := stringSet(current.RelevantIDs)
		relevantTotal += len(relevant)
		if len(relevant) > 0 {
			relevantCaseTotal++
		}
		firstRank, found, dcg := 0, 0, 0.0
		for index, id := range ranked {
			if _, ok := relevant[id]; !ok {
				continue
			}
			found++
			if firstRank == 0 {
				firstRank = index + 1
			}
			dcg += 1 / math.Log2(float64(index+2))
		}
		relevantFound += found
		hit := found > 0
		if current.ExpectNoRelevant {
			hit = len(ranked) == 0
		}
		if hit {
			hits++
			if firstRank > 0 {
				reciprocalRank += 1 / float64(firstRank)
			}
		}
		if len(ranked) == 0 {
			empty++
		}
		ideal := 0.0
		for index := 0; index < minInt(len(relevant), 5); index++ {
			ideal += 1 / math.Log2(float64(index+2))
		}
		caseNDCG := 0.0
		if ideal > 0 {
			caseNDCG = dcg / ideal
		}
		ndcg += caseNDCG
		stage.add(CaseResult{
			ID: current.ID, Passed: hit, LatencyMS: elapsed,
			FailureReason: failureIf(!hit, "no relevant document in top K"),
			Actual: map[string]any{
				"top_k_ids": ranked, "retrieval_mode": "deterministic_token_overlap_fixture",
				"rerank_mode": "none", "fallback_reason": "",
			},
			Verification: "executed",
		})
	}
	sort.Float64s(latencies)
	stage.Metrics["hit_rate_at_k"] = ratio(hits, stage.Total)
	stage.Metrics["recall_at_k"] = ratio(relevantFound, relevantTotal)
	stage.Metrics["mrr"] = average(reciprocalRank, relevantCaseTotal)
	stage.Metrics["ndcg_at_k"] = average(ndcg, relevantCaseTotal)
	stage.Metrics["average_latency_ms"] = average(latency, stage.Total)
	stage.Metrics["p95_latency_ms"] = percentile(latencies, 0.95)
	stage.Metrics["empty_recall_count"] = empty
	stage.Metrics["retrieval_mode"] = "deterministic_token_overlap_fixture"
	return stage
}

func evaluateFallback(cases []FallbackCase) StageReport {
	stage := newStage(EvalStageFallback, len(cases))
	for _, current := range cases {
		started := time.Now()
		code, limitation := fallbackOutcome(current.Failure)
		fallback := code != ""
		passed := fallback == current.ExpectedFallback &&
			code == current.ExpectedSafeCode &&
			limitation == current.ExpectedLimitation &&
			current.ExpectedNoPanic
		stage.add(CaseResult{
			ID: current.ID, Passed: passed,
			FailureReason: failureIf(!passed, "safe fallback contract did not match"),
			LatencyMS:     durationMS(started),
			Actual: map[string]any{
				"fallback_used": fallback, "safe_error_code": code,
				"limitation": limitation, "panic": false,
				"raw_error_exposed": false, "evidence_fabricated": false,
			},
			Verification: "contract_only",
		})
	}
	stage.Metrics["contract_pass_rate"] = ratio(stage.Passed, stage.Total)
	stage.Metrics["verified_case_count"] = 0
	return stage
}

func fallbackOutcome(failure string) (string, bool) {
	outcomes := map[string]string{
		"intent_llm_timeout":        "INTENT_RECOGNITION_FAILED",
		"intent_llm_invalid_json":   "INTENT_RECOGNITION_FAILED",
		"rule_recognizer_failure":   "INTENT_RECOGNITION_FAILED",
		"pre_rag_planner_failure":   "PRE_RAG_PLANNER_FALLBACK",
		"embedding_unavailable":     "VECTOR_RETRIEVAL_UNAVAILABLE",
		"external_reranker_failure": "RERANK_FALLBACK",
		"redis_unavailable":         "SESSION_MEMORY_UNAVAILABLE",
		"mysql_unavailable":         "LONG_TERM_MEMORY_UNAVAILABLE",
		"tool_timeout":              "TOOL_TIMEOUT",
		"tool_empty_result":         "TOOL_EMPTY_RESULT",
		"agent_invalid_json":        "AGENT_OUTPUT_PARSE_FAILED",
		"evidence_empty":            "NO_EVIDENCE",
		"sse_cancelled":             "CONTEXT_CANCELLED",
		"pre_rag_failure":           "PRE_RAG_UNAVAILABLE",
		"agent_model_failure":       "AGENT_LLM_FALLBACK",
	}
	code := outcomes[failure]
	return code, code != ""
}

func newStage(stage EvalStage, total int) StageReport {
	return StageReport{
		Stage: stage, Total: total, Verified: total, Metrics: map[string]any{},
		Cases: make([]CaseResult, 0, total),
	}
}

func (s *StageReport) add(result CaseResult) {
	if result.Verification == "contract_only" && s.Verified > 0 {
		s.Verified--
	}
	if result.Passed {
		s.Passed++
	} else {
		s.Failed++
	}
	s.Cases = append(s.Cases, result)
}

func focusView(value *Focus) intent.FocusView {
	if value == nil {
		return intent.FocusView{KnownSlots: map[string]string{}}
	}
	return intent.FocusView{
		LastIntent: value.LastIntent, KnownSlots: cloneSlots(value.KnownSlots),
		PendingQuestion: value.PendingQuestion,
		Candidates:      append([]string{}, value.Candidates...),
		TurnStatus:      value.TurnStatus, Available: true,
	}
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func average(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func durationMS(started time.Time) float64 {
	return float64(time.Since(started).Nanoseconds()) / 1e6
}

func mapContains(actual, expected map[string]string) bool {
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]string{}, left...)
	right = append([]string{}, right...)
	sort.Strings(left)
	sort.Strings(right)
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneSlots(source map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range source {
		result[key] = value
	}
	return result
}

func stringSet(values []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func roleStrings(values []multiagent.AgentRole) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func serviceCandidates(message string, existing []string) []string {
	result := []string{}
	lower := strings.ToLower(message)
	for _, service := range []string{"checkout", "payment", "order", "cart"} {
		if strings.Contains(lower, service) {
			result = append(result, service)
		}
	}
	if len(result) == 0 {
		return existing
	}
	return result
}

func rankRetrievalCorpus(
	query string,
	corpus []RetrievalDocument,
	limit int,
) []string {
	queryTokens := tokenSet(query)
	type scored struct {
		id    string
		score int
	}
	values := make([]scored, 0, len(corpus))
	for _, current := range corpus {
		score := 0
		for token := range tokenSet(current.Title + " " + current.Content) {
			if _, ok := queryTokens[token]; ok {
				score++
			}
		}
		if score > 0 {
			values = append(values, scored{id: current.ID, score: score})
		}
	}
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].score == values[j].score {
			return values[i].id < values[j].id
		}
		return values[i].score > values[j].score
	})
	if len(values) > limit {
		values = values[:limit]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.id)
	}
	return result
}

func tokenSet(value string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return r == ' ' || r == ',' || r == '-' || r == '_' || r == '/' ||
			r == '，' || r == '。'
	}) {
		if token != "" {
			result[token] = struct{}{}
		}
	}
	return result
}

func percentile(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(quantile*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func failureIf(condition bool, message string) string {
	if condition {
		return message
	}
	return ""
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
