package eino

import (
	"context"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestDeterministicRunnerRoutesErrorRateQuestion(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	output, err := NewDeterministicRunner(tools).Run(context.Background(), AgentInput{
		CurrentMessage: "Why did checkout error rate increase?",
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
	groups, ok := output.Metadata["evidence_groups"].(map[string]int)
	if !ok || groups["metrics"] != 1 || groups["logs"] != 1 {
		t.Fatalf("evidence groups = %#v, want metrics and logs", output.Metadata["evidence_groups"])
	}
	if output.Evidence[0].SourceType != "metrics" ||
		output.Evidence[1].SourceType != "logs" {
		t.Fatalf("evidence order = %#v, want source-grouped tool order", output.Evidence)
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
		CurrentMessage: "hello",
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

func TestDeterministicRunnerSupportsChineseQueryAndText(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	output, err := NewDeterministicRunner(tools).Run(
		context.Background(),
		AgentInput{
			CurrentMessage: "checkout 服务错误率为什么升高？请结合指标和日志分析。",
			TimeContext: common.TimeRange{
				From: "2026-06-30T00:00:00Z",
				To:   "2026-06-30T00:20:00Z",
			},
		},
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(output.ToolRuns) != 2 ||
		output.ToolRuns[0].Tool != "query_metrics" ||
		output.ToolRuns[1].Tool != "query_logs" {
		t.Fatalf("tool runs = %#v", output.ToolRuns)
	}
	if output.Metadata["response_language"] != "zh" {
		t.Fatalf("metadata = %#v", output.Metadata)
	}
	for _, conclusion := range output.Conclusions {
		if !prefersChinese(conclusion.Text) || len(conclusion.EvidenceIDs) == 0 {
			t.Fatalf("Chinese conclusion = %#v", conclusion)
		}
		for _, evidenceID := range conclusion.EvidenceIDs {
			if prefersChinese(evidenceID) {
				t.Fatalf("evidence ID was translated: %q", evidenceID)
			}
		}
	}
}

func TestDeterministicRunnerReportsLoadedSessionContext(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	output, err := NewDeterministicRunner(tools).Run(context.Background(), AgentInput{
		SessionSummary: session.Summary{
			Content: "Earlier checkout investigation",
			Version: 2,
		},
		RecentMessages: []session.Message{
			{Role: session.RoleUser, Content: "Previous question"},
		},
		CurrentMessage: "hello",
		TimeContext: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if output.Metadata["session_context_loaded"] != true ||
		output.Metadata["recent_message_count"] != 1 ||
		output.Metadata["summary_version"] != int64(2) {
		t.Fatalf("metadata = %#v, want loaded session context details", output.Metadata)
	}
}

func TestInferTraceID(t *testing.T) {
	const traceID = "9df0c1f254cffbe547fc944e821871d0"
	message := "Check trace " + traceID + " and explain the slow spans."

	if actual := inferTraceID(message); actual != traceID {
		t.Fatalf("inferTraceID() = %q, want %q", actual, traceID)
	}
}
