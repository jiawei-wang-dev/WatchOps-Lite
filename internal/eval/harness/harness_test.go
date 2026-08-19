package harness

import (
	"context"
	"os"
	"strings"
	"testing"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval/intenteval"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/evaluation"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func loadTestDatasets(t *testing.T) (Dataset, intenteval.Dataset) {
	t.Helper()
	file, err := os.Open("../../../testdata/oncall_eval_scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	dataset, err := LoadDataset(file)
	if err != nil {
		t.Fatal(err)
	}
	intentFile, err := os.Open("../../../testdata/intent_eval_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	defer intentFile.Close()
	intentDataset, err := intenteval.LoadDataset(intentFile)
	if err != nil {
		t.Fatal(err)
	}
	return dataset, intentDataset
}

func TestOfflineRetrievalReportsHitAndMiss(t *testing.T) {
	searcher := offlineSearcher{documents: []offlineDocument{{
		ID: "runbook-hit", Title: "checkout timeout runbook",
		Content: "payment deadline retry mitigation", Service: "checkout",
	}}, mode: "bm25"}
	report := evaluation.Evaluate(context.Background(), searcher, []evaluation.Case{
		{ID: "hit", Query: "checkout timeout", Service: "checkout", RelevantDocumentIDs: []string{"runbook-hit"}, ExpectedSourceType: "knowledge"},
		{ID: "miss", Query: "checkout timeout", Service: "checkout", RelevantDocumentIDs: []string{"missing"}, ExpectedSourceType: "knowledge"},
	}, 1)
	if report.Passed != 1 || report.Failed != 1 || !report.Cases[0].Hit || report.Cases[1].Hit {
		t.Fatalf("report = %#v", report)
	}
}

func TestScenarioToolSelectionAndArgumentsAreObserved(t *testing.T) {
	dataset, _ := loadTestDatasets(t)
	execution, err := executeScenario(context.Background(), dataset, dataset.Scenarios[4])
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(toolNames(execution.output.ToolRuns), ","); got != "query_traces" {
		t.Fatalf("tools = %s", got)
	}
	if ok, reason := argumentsMatch(execution.output.ToolRuns, dataset.Scenarios[4].ExpectedArguments); !ok {
		t.Fatalf("arguments mismatch: %s", reason)
	}
}

func TestValidateEvidenceAcceptsValidCitation(t *testing.T) {
	output := agenteino.AgentOutput{
		Evidence:    []common.EvidenceItem{{ID: "E1", SourceType: "metrics", Content: "observed"}},
		Conclusions: []agenteino.Conclusion{{Text: "observed", EvidenceIDs: []string{"E1"}}},
	}
	validation := ValidateEvidence(output, []string{"metrics"})
	if validation.ValidReferenceCount != 1 || len(validation.InvalidEvidenceIDs) != 0 {
		t.Fatalf("validation = %#v", validation)
	}
}

func TestValidateEvidenceRejectsInvalidCitationAndFailedToolEvidence(t *testing.T) {
	output := agenteino.AgentOutput{
		Conclusions: []agenteino.Conclusion{{Text: "unsupported", EvidenceIDs: []string{"missing"}}},
		ToolRuns:    []agenteino.ToolRun{{Tool: "query_logs", Success: false, EvidenceIDs: []string{"pseudo"}}},
	}
	validation := ValidateEvidence(output, []string{"logs"})
	if strings.Join(validation.InvalidEvidenceIDs, ",") != "missing" ||
		strings.Join(validation.FailedToolEvidenceIDs, ",") != "pseudo" {
		t.Fatalf("validation = %#v", validation)
	}
}

func TestDeterministicE2EFixtureAndReport(t *testing.T) {
	dataset, intentDataset := loadTestDatasets(t)
	report, err := Run(context.Background(), dataset, intentDataset)
	if err != nil {
		t.Fatal(err)
	}
	if report.ExternalLLM || report.AgentE2E.Cases != 17 || len(report.Retrieval) != 6 {
		t.Fatalf("report = %#v", report)
	}
	if report.Benchmarks.Clarification.RAGCalls != 0 || report.Benchmarks.Clarification.AgentCalls != 0 || report.Benchmarks.Clarification.ToolCalls != 0 {
		t.Fatalf("early exit = %#v", report.Benchmarks.Clarification)
	}
}

func TestRequiredAcceptableForbiddenToolSemantics(t *testing.T) {
	dataset, _ := loadTestDatasets(t)
	execution, err := executeScenario(context.Background(), dataset, dataset.Scenarios[0])
	if err != nil {
		t.Fatal(err)
	}
	metrics, cases, _ := evaluateToolEvidence(context.Background(), Dataset{Version: dataset.Version, FixedNow: dataset.FixedNow, Scenarios: dataset.Scenarios[:1]}, map[string]scenarioExecution{dataset.Scenarios[0].CaseID: execution})
	if metrics.RequiredToolRecall != 1 || metrics.ForbiddenToolCallCount != 0 || metrics.ToolPathValidity != 1 || !cases[0].TaskSuccess {
		t.Fatalf("metrics=%#v case=%#v", metrics, cases[0])
	}
}

func TestIntentChallengeProducesMeasuredFindingsWithoutExternalLLM(t *testing.T) {
	file, err := os.Open("../../../testdata/intent_challenge_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	dataset, err := LoadChallengeDataset(file)
	if err != nil {
		t.Fatal(err)
	}
	metrics, findings := evaluateChallenge(context.Background(), dataset)
	if metrics.Cases < 30 || metrics.IntentAccuracy <= 0 ||
		metrics.LLMEscalationRate <= 0 || metrics.IncorrectLLMEscalationRate != 0 ||
		len(findings) != 0 {
		t.Fatalf("metrics=%#v findings=%d", metrics, len(findings))
	}
}

func TestBadCaseReplayLoadsAndSortsStableIDs(t *testing.T) {
	cases, err := LoadBadCases(strings.NewReader(`[
		{"case_id":"b","stage":"tool_selection","expected":"x","actual":"y","reason":"r","timestamp":"2026-08-18T10:00:00Z"},
		{"case_id":"a","stage":"intent","expected":"x","actual":"y","reason":"r","timestamp":"2026-08-18T10:00:00Z"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if cases[0].CaseID != "a" || len(ReplayCaseIDs(cases)) != 2 {
		t.Fatalf("cases = %#v", cases)
	}
}
