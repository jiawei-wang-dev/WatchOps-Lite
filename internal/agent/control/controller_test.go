package control

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRepairJSONExtractsObjectOnce(t *testing.T) {
	controller := New(Config{EnableJSONRepairOnce: true})
	repaired, ok := controller.RepairJSON(context.Background(), "prefix {\"conclusions\":[],} suffix")
	if !ok {
		t.Fatal("RepairJSON() did not repair")
	}
	if strings.Contains(repaired, "prefix") || strings.Contains(repaired, ",}") {
		t.Fatalf("repaired = %q", repaired)
	}
}

func TestRepairJSONDisabled(t *testing.T) {
	controller := New(Config{EnableJSONRepairOnce: false})
	_, ok := controller.RepairJSON(context.Background(), "prefix {\"x\":1} suffix")
	if ok {
		t.Fatal("RepairJSON() repaired when disabled")
	}
}

func TestEvaluateControlsRepeatedToolFailures(t *testing.T) {
	controller := New(Config{
		MaxConsecutiveToolFailures:  2,
		MaxToolCalls:                10,
		TotalExecutionTimeout:       time.Second,
		EnableRepeatedToolDetection: true,
	})
	state := BuildState([]ToolRun{
		{Tool: "query_logs", Success: false, ErrorCode: "TOOL_TIMEOUT"},
		{Tool: "query_logs", Success: false, ErrorCode: "TOOL_TIMEOUT"},
	}, 0, 0, 20*time.Millisecond, true, false, 6)

	evaluation := controller.Evaluate(context.Background(), state)
	if !evaluation.ShouldFallback || evaluation.FailureReason != "consecutive_tool_failures" {
		t.Fatalf("evaluation = %#v, want controlled fallback", evaluation)
	}
}

func TestEvaluateAddsLimitationForEmptyEvidence(t *testing.T) {
	controller := New(DefaultConfig())
	state := BuildState(nil, 0, 0, 10*time.Millisecond, true, false, 6)

	evaluation := controller.Evaluate(context.Background(), state)
	if evaluation.ShouldFallback {
		t.Fatalf("evaluation = %#v, empty evidence should not force fallback", evaluation)
	}
	if len(evaluation.Limitations) == 0 || evaluation.Limitations[0].Code != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("limitations = %#v", evaluation.Limitations)
	}
}
