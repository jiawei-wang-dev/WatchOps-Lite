package multiagent

import (
	"context"
	"errors"
	"strings"
	"testing"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type longTermMemoryStub struct {
	memories []longterm.Memory
	err      error
}

func (s *longTermMemoryStub) Save(context.Context, longterm.Memory) error {
	return nil
}

func (s *longTermMemoryStub) Search(
	context.Context,
	longterm.SearchQuery,
) ([]longterm.Memory, error) {
	return s.memories, s.err
}

func (s *longTermMemoryStub) Get(context.Context, string) (longterm.Memory, error) {
	return longterm.Memory{}, errors.New("not implemented")
}

func TestKnowledgeAgentCombinesRunbookAndLongTermMemory(t *testing.T) {
	tools, err := agenteino.BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}
	agent, err := NewKnowledgeAgent(
		context.Background(),
		tools,
		&longTermMemoryStub{memories: []longterm.Memory{{
			ID:      "memory-1",
			Service: "checkout",
			Title:   "Previous checkout timeout",
			Summary: "Reducing unsafe retries stabilized checkout.",
		}}},
		3,
	)
	if err != nil {
		t.Fatalf("NewKnowledgeAgent() error = %v", err)
	}

	finding, err := agent.Analyze(context.Background(), TriagePlan{
		Service:      "checkout",
		IncidentType: IncidentHighErrorRate,
		EvidencePlan: []string{"knowledge"},
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
	if len(finding.ToolRuns) != 1 || !finding.ToolRuns[0].Success {
		t.Fatalf("ToolRuns = %#v", finding.ToolRuns)
	}
	if len(finding.Evidence) == 0 ||
		len(finding.EvidenceIDs) != len(finding.Evidence) {
		t.Fatalf("Evidence = %#v", finding.Evidence)
	}
	if finding.Metadata["long_term_memory_count"] != 1 {
		t.Fatalf("Metadata = %#v", finding.Metadata)
	}
	if finding.Metadata["long_term_memory_available"] != true ||
		finding.Metadata["long_term_memory_not_configured"] != false {
		t.Fatalf("Metadata = %#v", finding.Metadata)
	}
	if !strings.Contains(finding.Summary, "runbook:") ||
		!strings.Contains(finding.Summary, "memory:") {
		t.Fatalf("Summary = %q", finding.Summary)
	}
}

func TestKnowledgeAgentKeepsMemoryFailureAsLimitation(t *testing.T) {
	tools, err := agenteino.BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}
	agent, err := NewKnowledgeAgent(
		context.Background(),
		tools,
		&longTermMemoryStub{err: longterm.ErrUnavailable},
		3,
	)
	if err != nil {
		t.Fatalf("NewKnowledgeAgent() error = %v", err)
	}

	finding, err := agent.Analyze(context.Background(), TriagePlan{
		Service:      "checkout",
		EvidencePlan: []string{"knowledge"},
		Query:        "checkout runbook",
		Language:     "zh",
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(finding.Evidence) == 0 {
		t.Fatal("runbook evidence was lost when memory failed")
	}
	found := false
	for _, limitation := range finding.Limitations {
		if limitation.Code == "LONG_TERM_MEMORY_UNAVAILABLE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Limitations = %#v", finding.Limitations)
	}
	if finding.Metadata["long_term_memory_available"] != false ||
		finding.Metadata["long_term_memory_error"] != "search_failed" {
		t.Fatalf("Metadata = %#v", finding.Metadata)
	}
}

func TestKnowledgeAgentReportsLongTermMemoryConfiguredWithZeroMatches(t *testing.T) {
	agent, err := NewKnowledgeAgent(
		context.Background(),
		nil,
		&longTermMemoryStub{memories: []longterm.Memory{}},
		3,
	)
	if err != nil {
		t.Fatalf("NewKnowledgeAgent() error = %v", err)
	}
	finding, err := agent.Analyze(context.Background(), TriagePlan{
		Service:  "checkout",
		Query:    "checkout runbook",
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if finding.Metadata["long_term_memory_available"] != true ||
		finding.Metadata["long_term_memory_count"] != 0 {
		t.Fatalf("Metadata = %#v", finding.Metadata)
	}
}

func TestKnowledgeAgentReportsLongTermMemoryNotConfigured(t *testing.T) {
	agent, err := NewKnowledgeAgent(context.Background(), nil, nil, 3)
	if err != nil {
		t.Fatalf("NewKnowledgeAgent() error = %v", err)
	}
	finding, err := agent.Analyze(context.Background(), TriagePlan{
		Service:  "checkout",
		Query:    "checkout runbook",
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if finding.Metadata["long_term_memory_available"] != false ||
		finding.Metadata["long_term_memory_not_configured"] != true {
		t.Fatalf("Metadata = %#v", finding.Metadata)
	}
}

func TestKnowledgeAgentSkipsUnplannedRAG(t *testing.T) {
	agent, err := NewKnowledgeAgent(
		context.Background(),
		nil,
		nil,
		3,
	)
	if err != nil {
		t.Fatalf("NewKnowledgeAgent() error = %v", err)
	}
	finding, err := agent.Analyze(context.Background(), TriagePlan{
		Service:      "checkout",
		EvidencePlan: []string{"metrics"},
		Language:     "en",
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(finding.ToolRuns) != 0 ||
		finding.Metadata["knowledge_search_skipped"] != true {
		t.Fatalf("finding = %#v", finding)
	}
}
