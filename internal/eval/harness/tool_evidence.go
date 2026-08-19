package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/control"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/turngovernance"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/alerts"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/topology"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/traces"
)

type scenarioExecution struct {
	intentResult    intent.IntentResult
	decision        intent.IntentDecision
	output          agenteino.AgentOutput
	latency         time.Duration
	trace           []string
	retrievedIDs    []string
	retrievalHit    bool
	probeStopReason control.StopReason
}

type EvidenceValidation struct {
	ReferenceCount        int
	ValidReferenceCount   int
	InvalidEvidenceIDs    []string
	AllowlistViolations   []string
	FailedToolEvidenceIDs []string
}

// ValidateEvidence checks only structural provenance. It never asks an LLM to
// judge prose quality or invent missing evidence.
func ValidateEvidence(
	output agenteino.AgentOutput,
	allowedTypes []string,
) EvidenceValidation {
	result := EvidenceValidation{}
	known := map[string]string{}
	for _, item := range output.Evidence {
		known[item.ID] = item.SourceType
		if !containsString(allowedTypes, item.SourceType) {
			result.AllowlistViolations = append(result.AllowlistViolations, item.ID)
		}
	}
	for _, id := range citedEvidenceIDs(output) {
		result.ReferenceCount++
		if _, ok := known[id]; ok {
			result.ValidReferenceCount++
		} else {
			result.InvalidEvidenceIDs = append(result.InvalidEvidenceIDs, id)
		}
	}
	for _, run := range output.ToolRuns {
		if run.Success {
			continue
		}
		for _, id := range run.EvidenceIDs {
			result.FailedToolEvidenceIDs = append(result.FailedToolEvidenceIDs, id)
		}
	}
	sort.Strings(result.InvalidEvidenceIDs)
	sort.Strings(result.AllowlistViolations)
	sort.Strings(result.FailedToolEvidenceIDs)
	return result
}

func executeScenario(
	ctx context.Context,
	dataset Dataset,
	scenario Scenario,
) (scenarioExecution, error) {
	started := time.Now()
	now, _ := time.Parse(time.RFC3339, dataset.FixedNow)
	focus := focusFromMap(scenario.InitialContext)
	result, err := intent.NewRuleBasedRecognizer().Recognize(ctx, intent.RecognitionInput{
		Message: scenario.Input, Now: now, Focus: focus,
	})
	if err != nil {
		return scenarioExecution{}, err
	}
	decision := intent.ValidateSlots(
		scenario.Input, result, nil, focus, 0.55,
	)
	execution := scenarioExecution{
		intentResult: result,
		decision:     decision,
		trace:        []string{"intent", "slot_validation"},
	}
	if decision.Decision == intent.DecisionClarify {
		execution.output = agenteino.AgentOutput{
			Limitations: []agenteino.Limitation{{
				Code: "CLARIFICATION_REQUIRED", Message: decision.ClarifyQuestion,
			}},
			Metadata: map[string]any{"status": "clarification_required"},
		}
		execution.latency = time.Since(started)
		return execution, nil
	}
	tools, err := buildScenarioTools(scenario)
	if err != nil {
		return scenarioExecution{}, err
	}
	timeContext := turngovernance.ResolveTimeContext(common.TimeRange{}, result.TimeRange, now)
	_, documents := retrievalCases(dataset)
	retrieved, retrievalErr := (offlineSearcher{
		documents: documents,
		mode:      "hybrid",
		multi:     true,
		rerank:    true,
	}).Search(ctx, scenario.Input, 3, map[string]string{"service": result.Service})
	if retrievalErr != nil {
		return scenarioExecution{}, retrievalErr
	}
	for _, item := range retrieved {
		execution.retrievedIDs = append(execution.retrievedIDs, item.DocumentID)
		if containsString(scenario.RelevantDocumentIDs, item.DocumentID) {
			execution.retrievalHit = true
		}
	}
	preRetrieved := make([]retrievalknowledge.RetrievedKnowledge, 0, len(retrieved))
	for _, item := range retrieved {
		preRetrieved = append(preRetrieved, retrievalknowledge.RetrievedKnowledge{
			ID: item.DocumentID, DocumentID: item.DocumentID,
			Title: item.Title, Source: item.Source, Content: item.Content,
			Score: item.Score, RetrievalMethod: "offline_hybrid_rerank",
			Metadata: item.Metadata,
		})
	}
	execution.trace = append(
		execution.trace,
		"offline_rag", "tool", "evidence", "diagnosis",
	)
	execution.output, err = agenteino.NewDeterministicRunner(tools).Run(ctx, agenteino.AgentInput{
		CurrentMessage:        scenario.Input,
		Intent:                result,
		TimeContext:           timeContext,
		PreRetrievedKnowledge: preRetrieved,
		PreRAGAvailable:       true,
	})
	if scenario.RepeatedToolProbe != nil {
		probe := scenario.RepeatedToolProbe
		budget := control.NewExecutionBudget(control.Config{
			MaxToolCalls: 10, MaxRepeatedToolCalls: 2,
			EnableRepeatedToolDetection: true,
		})
		for attempt := 0; attempt < probe.Attempts; attempt++ {
			execution.probeStopReason = budget.BeforeToolCall(
				ctx, probe.Tool, probe.Arguments,
			)
			if execution.probeStopReason != "" {
				break
			}
		}
		if execution.output.Metadata == nil {
			execution.output.Metadata = map[string]any{}
		}
		execution.output.Metadata["stop_reason"] = string(execution.probeStopReason)
	}
	execution.latency = time.Since(started)
	return execution, err
}

func buildScenarioTools(scenario Scenario) ([]tool.InvokableTool, error) {
	resultFor := func(name string) (common.ToolResult, error) {
		behavior := scenario.ToolBehaviors[name]
		if behavior == "timeout" {
			return common.ToolResult{}, common.NewToolError(
				common.ErrorCodeTimeout, name, "fixture tool timed out", false,
				nil, "continue with an explicit timeout limitation",
			)
		}
		if behavior != "" && behavior != "empty" {
			return common.ToolResult{}, errors.New("unsupported fixture tool behavior: " + behavior)
		}
		items := scenario.Fixtures[name]
		if behavior == "empty" {
			items = nil
		}
		evidence := make([]common.EvidenceItem, 0, len(items))
		for _, item := range items {
			evidence = append(evidence, common.EvidenceItem{
				ID: item.ID, SourceType: item.SourceType,
				SourceName: "oncall_fixture", Content: item.Content,
			})
		}
		dataStatus := "available"
		if len(evidence) == 0 {
			dataStatus = "empty"
		}
		return common.ToolResult{
			Tool: name, Success: true, Evidence: evidence,
			Warnings: []common.ToolWarning{},
			Metadata: map[string]any{"mode": "fixture", "data_status": dataStatus},
		}, nil
	}
	metricTool, err := toolutils.InferTool(metrics.Name, "fixture metrics", func(context.Context, metrics.Input) (common.ToolResult, error) {
		return resultFor(metrics.Name)
	})
	if err != nil {
		return nil, err
	}
	logsTool, err := toolutils.InferTool(logs.Name, "fixture logs", func(context.Context, logs.Input) (common.ToolResult, error) {
		return resultFor(logs.Name)
	})
	if err != nil {
		return nil, err
	}
	traceTool, err := toolutils.InferTool(traces.Name, "fixture traces", func(context.Context, traces.Input) (common.ToolResult, error) {
		return resultFor(traces.Name)
	})
	if err != nil {
		return nil, err
	}
	knowledgeTool, err := toolutils.InferTool(knowledge.Name, "fixture knowledge", func(context.Context, knowledge.Input) (common.ToolResult, error) {
		return resultFor(knowledge.Name)
	})
	if err != nil {
		return nil, err
	}
	alertTool, err := toolutils.InferTool(alerts.Name, "fixture alerts", func(context.Context, alerts.Input) (common.ToolResult, error) {
		return resultFor(alerts.Name)
	})
	if err != nil {
		return nil, err
	}
	topologyTool, err := toolutils.InferTool(topology.Name, "fixture topology", func(context.Context, topology.Input) (common.ToolResult, error) {
		return resultFor(topology.Name)
	})
	if err != nil {
		return nil, err
	}
	return []tool.InvokableTool{logsTool, metricTool, alertTool, traceTool, knowledgeTool, topologyTool}, nil
}

func evaluateToolEvidence(
	ctx context.Context,
	dataset Dataset,
	executions map[string]scenarioExecution,
) (ToolEvidenceMetrics, []AgentCaseResult, []BadCase) {
	metricsResult := ToolEvidenceMetrics{
		Cases: len(dataset.Scenarios), ArgumentFieldAccuracy: map[string]float64{},
	}
	results := make([]AgentCaseResult, 0, len(dataset.Scenarios))
	badCases := []BadCase{}
	fixedNow, _ := timeFromDataset(dataset)
	selectionMatches, argumentMatches, validPaths := 0, 0, 0
	requiredHits, requiredTotal := 0, 0
	unnecessaryCalls, totalCalls := 0, 0
	evidenceAllowed, evidenceTotal := 0, 0
	evidenceCovered, evidenceRequired := 0, 0
	fieldHits, fieldTotals := map[string]int{}, map[string]int{}
	validCitations, citationCount := 0, 0
	for _, scenario := range dataset.Scenarios {
		execution := executions[scenario.CaseID]
		actualTools := toolNames(execution.output.ToolRuns)
		selectionOK := sameStrings(actualTools, scenario.RequiredTools)
		if selectionOK {
			selectionMatches++
		}
		requiredMissing := missingStrings(scenario.RequiredTools, actualTools)
		requiredTotal += len(scenario.RequiredTools)
		requiredHits += len(scenario.RequiredTools) - len(requiredMissing)
		forbiddenSet := stringSet(scenario.ForbiddenTools)
		allowedSet := stringSet(append(append([]string{}, scenario.RequiredTools...), scenario.AcceptableTools...))
		forbiddenCalls := []string{}
		unnecessary := []string{}
		for _, name := range actualTools {
			totalCalls++
			if _, ok := forbiddenSet[name]; ok {
				metricsResult.ForbiddenToolCallCount++
				forbiddenCalls = append(forbiddenCalls, name)
			}
			if _, ok := allowedSet[name]; !ok {
				unnecessaryCalls++
				unnecessary = append(unnecessary, name)
			}
		}
		pathOK := len(requiredMissing) == 0 && len(forbiddenCalls) == 0 && len(unnecessary) == 0
		if pathOK {
			validPaths++
		} else {
			badCases = append(badCases, BadCase{
				CaseID: scenario.CaseID, Stage: "tool_selection",
				Expected: map[string]any{"required": scenario.RequiredTools, "acceptable": scenario.AcceptableTools, "forbidden": scenario.ForbiddenTools},
				Actual:   actualTools, Reason: fmt.Sprintf("missing=%v forbidden=%v unnecessary=%v", requiredMissing, forbiddenCalls, unnecessary), Timestamp: fixedNow,
			})
		}
		fieldResults := argumentFieldResults(execution.output.ToolRuns, scenario.ExpectedArguments, scenario.QuerySemanticTerms)
		argumentsOK, argumentReason := summarizeArgumentFields(fieldResults)
		for field, ok := range fieldResults {
			fieldTotals[field]++
			if ok {
				fieldHits[field]++
			}
		}
		if argumentsOK {
			argumentMatches++
		} else {
			badCases = append(badCases, BadCase{
				CaseID: scenario.CaseID, Stage: "tool_arguments",
				Expected: scenario.ExpectedArguments, Actual: normalizedArguments(execution.output.ToolRuns),
				Reason: argumentReason, Timestamp: fixedNow,
			})
		}
		allowedEvidence := append(append([]string{}, scenario.RequiredEvidenceTypes...), scenario.AcceptableEvidenceTypes...)
		validation := ValidateEvidence(execution.output, allowedEvidence)
		citationCount += validation.ReferenceCount
		validCitations += validation.ValidReferenceCount
		evidenceTotal += len(execution.output.Evidence)
		evidenceAllowed += len(execution.output.Evidence) - len(validation.AllowlistViolations)
		metricsResult.EvidenceAllowlistViolationCount += len(validation.AllowlistViolations)
		metricsResult.EvidenceAllowlistViolationCount += len(validation.FailedToolEvidenceIDs)
		actualEvidenceTypes := []string{}
		for _, item := range execution.output.Evidence {
			actualEvidenceTypes = append(actualEvidenceTypes, item.SourceType)
		}
		missingEvidence := missingStrings(scenario.RequiredEvidenceTypes, actualEvidenceTypes)
		evidenceRequired += len(scenario.RequiredEvidenceTypes)
		evidenceCovered += len(scenario.RequiredEvidenceTypes) - len(missingEvidence)
		for _, id := range validation.InvalidEvidenceIDs {
			badCases = append(badCases, BadCase{
				CaseID: scenario.CaseID, Stage: "evidence", Expected: "existing evidence_id",
				Actual: id, Reason: "answer cited evidence that was not generated", Timestamp: fixedNow,
			})
		}
		observedStatus := "completed"
		if execution.decision.Decision == intent.DecisionClarify {
			observedStatus = "clarified"
		} else if execution.probeStopReason == control.StopReasonRepeatedToolCall {
			observedStatus = "repeated_tool_call"
		} else {
			for _, run := range execution.output.ToolRuns {
				if !run.Success {
					if run.ErrorCode == common.ErrorCodeTimeout {
						observedStatus = "timeout"
					} else {
						observedStatus = "tool_failure"
					}
					break
				}
			}
			if observedStatus == "completed" && len(execution.output.Evidence) == 0 {
				observedStatus = "no_evidence"
			}
		}
		failureReasons := []string{}
		if string(execution.intentResult.Intent) != scenario.ExpectedIntent {
			failureReasons = append(failureReasons, "intent")
		}
		if string(execution.decision.Decision) != scenario.ExpectedDecision {
			failureReasons = append(failureReasons, "decision")
		}
		if !pathOK {
			failureReasons = append(failureReasons, "tool_path")
		}
		if !argumentsOK {
			failureReasons = append(failureReasons, "tool_arguments")
		}
		if len(missingEvidence) > 0 {
			failureReasons = append(failureReasons, "evidence_coverage")
		}
		if len(validation.InvalidEvidenceIDs)+len(validation.AllowlistViolations)+len(validation.FailedToolEvidenceIDs) > 0 {
			failureReasons = append(failureReasons, "evidence_validity")
		}
		expectedStatus := scenario.ExpectedStatus
		if expectedStatus == "" {
			if scenario.ExpectedDecision == string(intent.DecisionClarify) {
				expectedStatus = "clarified"
			} else {
				expectedStatus = "completed"
			}
		}
		if observedStatus != expectedStatus {
			failureReasons = append(failureReasons, "terminal_status")
		}
		if scenario.RetrievalExpectation == "hit" && !execution.retrievalHit {
			failureReasons = append(failureReasons, "retrieval_hit")
		}
		if scenario.RetrievalExpectation == "miss" && execution.retrievalHit {
			failureReasons = append(failureReasons, "unexpected_retrieval_hit")
		}
		taskSuccess := len(failureReasons) == 0
		finalStatus := "passed"
		if !taskSuccess {
			finalStatus = "failed"
		}
		caseEvidenceCoverage := 1.0
		if len(scenario.RequiredEvidenceTypes) > 0 {
			caseEvidenceCoverage = ratio(len(scenario.RequiredEvidenceTypes)-len(missingEvidence), len(scenario.RequiredEvidenceTypes))
		}
		results = append(results, AgentCaseResult{
			CaseID: scenario.CaseID, Category: scenario.Category, Input: scenario.Input,
			InitialContext: scenario.InitialContext, ExpectedIntent: scenario.ExpectedIntent,
			ExpectedSlots: scenario.ExpectedSlots, RequiredTools: scenario.RequiredTools,
			AcceptableTools: scenario.AcceptableTools, ForbiddenTools: scenario.ForbiddenTools,
			RequiredEvidenceTypes: scenario.RequiredEvidenceTypes,
			ExpectedDecision:      scenario.ExpectedDecision,
			ActualTrace:           append([]string{}, execution.trace...),
			ActualTools:           actualTools, ActualEvidence: evidenceIDs(execution.output),
			FinalStatus: finalStatus, TaskSuccess: taskSuccess, ObservedStatus: observedStatus,
			DecisionCorrect:          string(execution.decision.Decision) == scenario.ExpectedDecision,
			RequiredEvidenceCoverage: caseEvidenceCoverage,
			UnsafeAction:             len(forbiddenCalls) > 0, RepeatedToolCallStop: execution.probeStopReason == control.StopReasonRepeatedToolCall,
			FailureReason: strings.Join(failureReasons, ","),
			LatencyMS:     float64(execution.latency.Nanoseconds()) / 1e6,
		})
	}
	metricsResult.ToolSelectionAccuracy = ratio(selectionMatches, len(dataset.Scenarios))
	metricsResult.ToolArgumentAccuracy = ratio(argumentMatches, len(dataset.Scenarios))
	metricsResult.RequiredToolRecall = ratio(requiredHits, requiredTotal)
	metricsResult.ForbiddenToolViolationRate = ratio(metricsResult.ForbiddenToolCallCount, totalCalls)
	metricsResult.UnnecessaryToolCallRate = ratio(unnecessaryCalls, totalCalls)
	metricsResult.ToolPathValidity = ratio(validPaths, len(dataset.Scenarios))
	metricsResult.EvidenceCitationValidity = ratio(validCitations, citationCount)
	metricsResult.EvidenceCoverage = ratio(evidenceCovered, evidenceRequired)
	metricsResult.EvidenceAllowlistViolationRate = ratio(metricsResult.EvidenceAllowlistViolationCount, evidenceTotal)
	if evidenceTotal == 0 {
		metricsResult.EvidenceAllowlistViolationRate = 0
	} else if evidenceAllowed < 0 {
		metricsResult.EvidenceAllowlistViolationRate = 1
	}
	for field, total := range fieldTotals {
		metricsResult.ArgumentFieldAccuracy[field] = ratio(fieldHits[field], total)
	}
	return metricsResult, results, badCases
}

func focusFromMap(value map[string]any) intent.FocusView {
	result := intent.FocusView{KnownSlots: map[string]string{}}
	if len(value) == 0 {
		return result
	}
	result.Available = boolValue(value["available"])
	result.LastIntent = stringValue(value["last_intent"])
	result.PendingQuestion = stringValue(value["pending_question"])
	result.TurnStatus = stringValue(value["turn_status"])
	result.Summary = stringValue(value["summary"])
	if slots, ok := value["known_slots"].(map[string]any); ok {
		for key, item := range slots {
			result.KnownSlots[key] = stringValue(item)
		}
	}
	if slots, ok := value["known_slots"].(map[string]string); ok {
		for key, item := range slots {
			result.KnownSlots[key] = item
		}
	}
	if candidates, ok := value["candidates"].([]any); ok {
		for _, item := range candidates {
			result.Candidates = append(result.Candidates, stringValue(item))
		}
	}
	return result
}

func boolValue(value any) bool     { result, _ := value.(bool); return result }
func stringValue(value any) string { result, _ := value.(string); return result }

func missingStrings(required, actual []string) []string {
	missing := []string{}
	for _, item := range required {
		if !containsString(actual, item) {
			missing = append(missing, item)
		}
	}
	return missing
}

func argumentFieldResults(runs []agenteino.ToolRun, expected map[string]string, semanticTerms []string) map[string]bool {
	result := map[string]bool{}
	for key, value := range expected {
		matched := false
		for _, run := range runs {
			args := strings.ToLower(run.NormalizedArgs)
			switch key {
			case "service":
				matched = matched || sameService(run.Service, value) || strings.Contains(args, strings.ToLower(value))
			case "trace_id":
				matched = matched || strings.Contains(args, strings.ToLower(value))
			case "time_range":
				matched = matched || timeRangeMatches(run.TimeRange, value)
			case "symptom", "query":
				matched = matched || semanticArgumentMatch(args, value)
			default:
				matched = matched || strings.Contains(args, strings.ToLower(value))
			}
		}
		result[key] = matched
	}
	if len(semanticTerms) > 0 {
		matched := false
		for _, run := range runs {
			args := strings.ToLower(run.NormalizedArgs)
			all := true
			for _, term := range semanticTerms {
				all = all && semanticArgumentMatch(args, term)
			}
			matched = matched || all
		}
		result["query_semantic"] = matched
	}
	return result
}

func summarizeArgumentFields(fields map[string]bool) (bool, string) {
	missing := []string{}
	for key, ok := range fields {
		if !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) == 0 {
		return true, ""
	}
	return false, "missing semantic argument fields: " + strings.Join(missing, ",")
}

func semanticArgumentMatch(args, expected string) bool {
	args, expected = strings.ToLower(args), strings.ToLower(expected)
	if strings.Contains(args, expected) {
		return true
	}
	groups := [][]string{{"error", "errors", "错误", "失败", "5xx", "exception"}, {"latency", "slow", "延迟", "耗时", "慢"}, {"timeout", "超时", "deadline"}, {"connection", "连接", "refused", "pool"}, {"database", "数据库", "db", "mysql"}}
	for _, group := range groups {
		expectedInGroup, argsInGroup := false, false
		for _, word := range group {
			expectedInGroup = expectedInGroup || strings.Contains(expected, word)
			argsInGroup = argsInGroup || strings.Contains(args, word)
		}
		if expectedInGroup && argsInGroup {
			return true
		}
	}
	return false
}

func toolNames(runs []agenteino.ToolRun) []string {
	result := make([]string, 0, len(runs))
	for _, run := range runs {
		result = append(result, run.Tool)
	}
	sort.Strings(result)
	return result
}

func normalizedArguments(runs []agenteino.ToolRun) map[string]string {
	result := map[string]string{}
	for _, run := range runs {
		result[run.Tool] = run.NormalizedArgs
	}
	return result
}

func argumentsMatch(runs []agenteino.ToolRun, expected map[string]string) (bool, string) {
	for key, value := range expected {
		matched := false
		for _, run := range runs {
			switch key {
			case "service":
				matched = matched || sameService(run.Service, value)
			case "trace_id":
				matched = matched || strings.Contains(strings.ToLower(run.NormalizedArgs), strings.ToLower(value))
			case "time_range":
				matched = matched || timeRangeMatches(run.TimeRange, value)
			default:
				matched = matched || strings.Contains(strings.ToLower(run.NormalizedArgs), strings.ToLower(value))
			}
		}
		if !matched {
			return false, fmt.Sprintf("no selected tool carried expected %s=%s", key, value)
		}
	}
	return true, ""
}

func timeRangeMatches(value *common.TimeRange, expected string) bool {
	if value == nil {
		return false
	}
	from, fromErr := time.Parse(time.RFC3339, value.From)
	to, toErr := time.Parse(time.RFC3339, value.To)
	if fromErr != nil || toErr != nil {
		return false
	}
	var minutes int
	if _, err := fmt.Sscanf(expected, "last_%d_minutes", &minutes); err != nil {
		return false
	}
	return to.Sub(from) == time.Duration(minutes)*time.Minute
}

func citedEvidenceIDs(output agenteino.AgentOutput) []string {
	result := []string{}
	for _, item := range output.Conclusions {
		result = append(result, item.EvidenceIDs...)
	}
	for _, item := range output.Inferences {
		result = append(result, item.EvidenceIDs...)
	}
	for _, item := range output.Recommendations {
		result = append(result, item.EvidenceIDs...)
	}
	return result
}

func evidenceIDs(output agenteino.AgentOutput) []string {
	result := make([]string, 0, len(output.Evidence))
	for _, item := range output.Evidence {
		result = append(result, item.ID)
	}
	sort.Strings(result)
	return result
}

func sameStrings(left, right []string) bool {
	a := append([]string{}, left...)
	b := append([]string{}, right...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

func stringSet(values []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func metadataJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
