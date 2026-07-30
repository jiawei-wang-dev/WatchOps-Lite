package nodeeval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDatasetRejectsEmptyAndInvalidData(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"empty", `{}`},
		{"invalid intent", `{
			"version":"v1","fixed_now":"2026-07-30T00:00:00Z",
			"intent":[{"id":"x","message":"x","expected_intent":"made_up"}]
		}`},
		{"duplicate id", `{
			"version":"v1","fixed_now":"2026-07-30T00:00:00Z",
			"intent":[
				{"id":"x","message":"x","expected_intent":"general_chat"},
				{"id":"x","message":"y","expected_intent":"general_chat"}
			]
		}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := LoadDataset(strings.NewReader(test.json)); err == nil {
				t.Fatal("LoadDataset() error = nil")
			}
		})
	}
}

func TestDatasetCountsAndEvaluationMetrics(t *testing.T) {
	file, err := os.Open("../../../testdata/node_eval_cases.json")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	defer file.Close()
	dataset, err := LoadDataset(file)
	if err != nil {
		t.Fatalf("LoadDataset() error = %v", err)
	}
	if len(dataset.Intent) < 30 || len(dataset.Slot) < 20 ||
		len(dataset.Context) < 15 || len(dataset.Routing) < 20 ||
		len(dataset.Fallback) < 15 {
		t.Fatalf("dataset counts are below required minimum: %#v", dataset)
	}
	report := Evaluate(context.Background(), dataset)
	if report.Stages[EvalStageSlot].Metrics["clarification_precision"].(float64) <= 0 ||
		report.Stages[EvalStageRouting].Metrics["per_role_recall"] == nil {
		t.Fatalf("metrics = %#v", report.Stages)
	}
	if report.Stages[EvalStageContext].Metrics["isolated_session_count"] != 15 {
		t.Fatalf("context metrics = %#v", report.Stages[EvalStageContext].Metrics)
	}
	if len(report.Stages) != 5 {
		t.Fatalf("stage count = %d, want 5", len(report.Stages))
	}
	executed := len(dataset.Intent) + len(dataset.Slot) +
		len(dataset.Context) + len(dataset.Routing)
	declared := executed + len(dataset.Fallback)
	if report.Summary["declared_case_total"] != declared ||
		report.Summary["total"] != executed ||
		report.Summary["passed"] != executed ||
		report.Summary["contract_only_case_count"] != len(dataset.Fallback) {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestWriteReportsAndOptionalThresholds(t *testing.T) {
	report := Report{
		DatasetVersion: "v1",
		Summary:        map[string]any{},
		Stages: map[EvalStage]StageReport{
			EvalStageIntent: {
				Stage: EvalStageIntent,
				Total: 1, Passed: 1,
				Metrics: map[string]any{"accuracy": 0.8},
			},
			EvalStageRouting: {
				Metrics: map[string]any{"exact_match": 1.0},
			},
			EvalStageFallback: {
				Metrics: map[string]any{"contract_pass_rate": 1.0},
			},
		},
	}
	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "report.json")
	markdownPath := filepath.Join(directory, "report.md")
	if err := WriteReports(report, jsonPath, markdownPath); err != nil {
		t.Fatalf("WriteReports() error = %v", err)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("JSON report: %v", err)
	}
	if data, err := os.ReadFile(markdownPath); err != nil ||
		!strings.Contains(string(data), "WatchOps Node Eval Report") {
		t.Fatalf("Markdown report data=%q error=%v", data, err)
	}
	if failures := ThresholdFailures(report, func(string) string { return "" }); len(failures) != 0 {
		t.Fatalf("thresholds disabled failures=%v", failures)
	}
	failures := ThresholdFailures(report, func(key string) string {
		if key == "WATCHOPS_INTENT_ACCURACY_MIN" {
			return "0.9"
		}
		return ""
	})
	if len(failures) != 1 {
		t.Fatalf("threshold failures=%v", failures)
	}
}
