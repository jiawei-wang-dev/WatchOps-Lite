package intenteval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDatasetRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"empty", `{}`},
		{"invalid intent", `{
			"version":"v1","fixed_now":"2026-07-30T00:00:00Z",
			"cases":[{"id":"x","message":"x","expected_intent":"made_up"}]
		}`},
		{"duplicate id", `{
			"version":"v1","fixed_now":"2026-07-30T00:00:00Z",
			"cases":[
				{"id":"x","message":"x","expected_intent":"general_chat"},
				{"id":"x","message":"y","expected_intent":"general_chat"}
			]
		}`},
		{"unsupported slot", `{
			"version":"v1","fixed_now":"2026-07-30T00:00:00Z",
			"cases":[{
				"id":"x","message":"x","expected_intent":"general_chat",
				"expected_slots":{"tenant":"production"}
			}]
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

func TestDatasetEvaluationAndMetrics(t *testing.T) {
	file, err := os.Open("../../../testdata/intent_eval_cases.json")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	defer file.Close()
	dataset, err := LoadDataset(file)
	if err != nil {
		t.Fatalf("LoadDataset() error = %v", err)
	}
	report := Evaluate(context.Background(), dataset)
	if report.Total != len(dataset.Cases) ||
		report.Passed+report.Failed != report.Total {
		t.Fatalf("report totals = %#v", report)
	}
	if report.Metrics.IntentAccuracy <= 0 ||
		report.Metrics.SlotFieldAccuracy <= 0 ||
		report.Metrics.JointIntentSlotExactMatch <= 0 {
		t.Fatalf("metrics = %#v", report.Metrics)
	}
	for _, field := range slotFields {
		if _, exists := report.Metrics.SlotFieldAccuracyByField[field]; !exists {
			t.Fatalf("missing field accuracy for %q", field)
		}
	}
}

func TestWriteReportsAndOptionalThresholds(t *testing.T) {
	report := Report{
		DatasetVersion: "v1",
		Total:          1,
		Passed:         1,
		Metrics: Metrics{
			IntentAccuracy:            0.8,
			SlotFieldAccuracy:         0.75,
			JointIntentSlotExactMatch: 0.7,
			SlotFieldAccuracyByField: map[string]float64{
				"service": 1, "time_range": 1, "trace_id": 1, "symptom": 0,
			},
		},
	}
	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "report.json")
	markdownPath := filepath.Join(directory, "report.md")
	if err := WriteReports(report, jsonPath, markdownPath); err != nil {
		t.Fatalf("WriteReports() error = %v", err)
	}
	data, err := os.ReadFile(markdownPath)
	if err != nil || !strings.Contains(string(data), "Intent Accuracy") {
		t.Fatalf("Markdown report data=%q error=%v", data, err)
	}
	if failures := ThresholdFailures(
		report,
		func(string) string { return "" },
	); len(failures) != 0 {
		t.Fatalf("thresholds disabled failures=%v", failures)
	}
	failures := ThresholdFailures(report, func(key string) string {
		if key == "WATCHOPS_JOINT_INTENT_SLOT_MIN" {
			return "0.8"
		}
		return ""
	})
	if len(failures) != 1 {
		t.Fatalf("threshold failures=%v", failures)
	}
}
