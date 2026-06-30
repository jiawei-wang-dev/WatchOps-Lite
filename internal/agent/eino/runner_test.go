package eino

import (
	"context"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestDeterministicRunnerRoutesErrorRateQuestion(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	output, err := NewDeterministicRunner(tools).Run(context.Background(), AgentInput{
		Message: "Why did checkout error rate increase?",
		TimeContext: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(output.ToolRuns) != 2 {
		t.Fatalf("tool run count = %d, want 2", len(output.ToolRuns))
	}
	if output.ToolRuns[0].Tool != "query_metrics" || output.ToolRuns[1].Tool != "query_logs" {
		t.Fatalf("tool runs = %#v, want metrics then logs", output.ToolRuns)
	}
	if len(output.Evidence) != 2 || len(output.Conclusions) != 2 {
		t.Fatalf("output = %#v, want two evidence-backed conclusions", output)
	}
	for _, conclusion := range output.Conclusions {
		if len(conclusion.EvidenceIDs) == 0 {
			t.Fatalf("conclusion has no evidence IDs: %#v", conclusion)
		}
	}
}

func TestDeterministicRunnerReturnsLimitationWhenNoRouteMatches(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	output, err := NewDeterministicRunner(tools).Run(context.Background(), AgentInput{
		Message: "hello",
		TimeContext: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(output.ToolRuns) != 0 || len(output.Evidence) != 0 {
		t.Fatalf("output = %#v, want no tool execution or evidence", output)
	}
	if len(output.Limitations) != 1 || output.Limitations[0].Code != "MORE_CONTEXT_REQUIRED" {
		t.Fatalf("limitations = %#v, want MORE_CONTEXT_REQUIRED", output.Limitations)
	}
}
