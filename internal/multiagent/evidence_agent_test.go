package multiagent

import (
	"context"
	"strings"
	"testing"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestEvidenceAgentExecutesOnlyPlannedObservabilitySources(t *testing.T) {
	tools, err := agenteino.BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}
	agent, err := NewEvidenceAgent(context.Background(), tools)
	if err != nil {
		t.Fatalf("NewEvidenceAgent() error = %v", err)
	}

	finding, err := agent.Analyze(context.Background(), TriagePlan{
		Service:      "checkout",
		IncidentType: IncidentHighErrorRate,
		EvidencePlan: []string{"metrics", "logs", "alerts", "knowledge"},
		Query:        "checkout error rate runbook",
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if finding.Role != AgentRoleEvidence {
		t.Fatalf("Role = %q, want evidence", finding.Role)
	}
	if len(finding.ToolRuns) != 3 {
		t.Fatalf("ToolRuns = %d, want 3 observation tools", len(finding.ToolRuns))
	}
	for _, run := range finding.ToolRuns {
		if run.Tool == "search_knowledge" {
			t.Fatal("Evidence Agent executed Knowledge Agent tool")
		}
		if !run.Success {
			t.Fatalf("tool run = %#v, want success", run)
		}
	}
	if len(finding.Evidence) == 0 ||
		len(finding.EvidenceIDs) != len(finding.Evidence) {
		t.Fatalf("finding evidence = %#v", finding)
	}
	if !strings.Contains(finding.Summary, "Observability summary") {
		t.Fatalf("Summary = %q", finding.Summary)
	}
}

func TestEvidenceAgentTurnsMissingToolIntoLimitation(t *testing.T) {
	tools, err := agenteino.BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}
	agent, err := NewEvidenceAgent(context.Background(), tools[:1])
	if err != nil {
		t.Fatalf("NewEvidenceAgent() error = %v", err)
	}

	finding, err := agent.Analyze(context.Background(), TriagePlan{
		Service:      "checkout",
		IncidentType: IncidentHighErrorRate,
		EvidencePlan: []string{"metrics", "logs"},
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
		Language: "zh",
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(finding.ToolRuns) != 2 {
		t.Fatalf("ToolRuns = %d, want 2", len(finding.ToolRuns))
	}
	if len(finding.Limitations) != 1 ||
		finding.Limitations[0].Code != "EVIDENCE_TOOL_UNAVAILABLE" ||
		finding.Limitations[0].Tool != "query_metrics" {
		t.Fatalf("Limitations = %#v", finding.Limitations)
	}
	if len(finding.Evidence) == 0 {
		t.Fatal("remaining logs evidence was not preserved")
	}
}
