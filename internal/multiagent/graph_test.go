package multiagent

import (
	"context"
	"sync"
	"testing"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type fakeTriagePlanner struct{}

func (fakeTriagePlanner) Plan(
	_ context.Context,
	input Input,
) (TriagePlan, error) {
	return TriagePlan{
		Service:      "checkout",
		IncidentType: "high_error_rate",
		EvidencePlan: []string{"metrics", "knowledge"},
		Query:        input.Message,
		TimeContext:  input.TimeContext,
		Language:     "en",
	}, nil
}

type llmMetadataTriagePlanner struct{}

func (llmMetadataTriagePlanner) Plan(
	_ context.Context,
	input Input,
) (TriagePlan, error) {
	return TriagePlan{
		Service:      "checkout",
		IncidentType: "high_error_rate",
		EvidencePlan: []string{"metrics", "knowledge"},
		Query:        input.Message,
		TimeContext:  input.TimeContext,
		Language:     "en",
		Metadata: map[string]any{
			"triage_llm_used":        true,
			"triage_llm_attempted":   true,
			"triage_model":           "test-model",
			"triage_fallback_used":   false,
			"triage_llm_duration_ms": int64(5),
			"triage_mode":            "llm",
		},
	}, nil
}

type recordingAnalyzer struct {
	role AgentRole
	mu   *sync.Mutex
	seen *[]TriagePlan
}

func (a recordingAnalyzer) Analyze(
	_ context.Context,
	plan TriagePlan,
) (AgentFinding, error) {
	a.mu.Lock()
	*a.seen = append(*a.seen, plan)
	a.mu.Unlock()
	id := string(a.role) + "-1"
	return AgentFinding{
		Role:        a.role,
		Summary:     string(a.role) + " summary",
		EvidenceIDs: []string{id},
		Evidence: []common.EvidenceItem{{
			ID: id,
			SourceType: map[AgentRole]string{
				AgentRoleEvidence:  "metrics",
				AgentRoleKnowledge: "knowledge",
			}[a.role],
			SourceName: string(a.role),
			Content:    string(a.role) + " evidence",
		}},
	}, nil
}

type fakeSynthesizer struct{}

func (fakeSynthesizer) Synthesize(
	_ context.Context,
	input SynthesisInput,
) (agenteino.AgentOutput, error) {
	return agenteino.AgentOutput{
		Conclusions: []agenteino.Conclusion{{
			Text:        "Combined diagnosis",
			EvidenceIDs: []string{"evidence-1", "knowledge-1"},
		}},
		Evidence:    input.Evidence,
		ToolRuns:    input.ToolRuns,
		Limitations: input.Limitations,
	}, nil
}

func TestOrchestratorRunsNativeEinoFanOutAndFanIn(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []TriagePlan
	)
	orchestrator := NewOrchestrator(
		context.Background(),
		fakeTriagePlanner{},
		recordingAnalyzer{role: AgentRoleEvidence, mu: &mu, seen: &seen},
		recordingAnalyzer{role: AgentRoleKnowledge, mu: &mu, seen: &seen},
		fakeSynthesizer{},
	)
	var tick int64
	orchestrator.now = func() time.Time {
		tick++
		return time.UnixMilli(tick * 10).UTC()
	}

	result, err := orchestrator.Execute(context.Background(), Input{
		RequestID: "req-1",
		SessionID: "session-1",
		Message:   "Why is checkout failing?",
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("analyzers saw %d plans, want 2", len(seen))
	}
	for _, plan := range seen {
		if plan.Service != "checkout" || plan.Query != "Why is checkout failing?" {
			t.Fatalf("analyzer plan = %#v", plan)
		}
	}
	if len(result.Steps) != 4 {
		t.Fatalf("Steps = %d, want 4", len(result.Steps))
	}
	for index, role := range RoleOrder() {
		if result.Steps[index].Role != role ||
			result.Steps[index].Status != AgentStepCompleted {
			t.Fatalf("step %d = %#v, want completed %s", index, result.Steps[index], role)
		}
	}
	if len(result.Evidence) != 2 ||
		len(result.FinalAnswer.Conclusions) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Metadata["agent_mode"] != "multi_agent" ||
		result.Metadata["orchestrator"] != "eino_graph" {
		t.Fatalf("Metadata = %#v", result.Metadata)
	}
	if result.Metadata["multi_agent_llm_used"] != false ||
		result.Metadata["multi_agent_llm_call_count"] != 0 ||
		result.Metadata["triage_fallback_used"] != true ||
		result.Metadata["evidence_fallback_used"] != true ||
		result.Metadata["knowledge_fallback_used"] != true ||
		result.Metadata["synthesis_fallback_used"] != true {
		t.Fatalf("deterministic LLM metadata = %#v", result.Metadata)
	}
}

func TestOrchestratorDoesNotCompileWithoutAllRoles(t *testing.T) {
	orchestrator := NewOrchestrator(
		context.Background(),
		fakeTriagePlanner{},
		recordingAnalyzer{},
		nil,
		fakeSynthesizer{},
	)

	if _, err := orchestrator.Execute(context.Background(), Input{}); err == nil {
		t.Fatal("Execute() error = nil, want unavailable graph error")
	}
}

type llmMetadataAnalyzer struct {
	role AgentRole
}

func (a llmMetadataAnalyzer) Analyze(
	_ context.Context,
	_ TriagePlan,
) (AgentFinding, error) {
	prefix := string(a.role)
	id := prefix + "-1"
	return AgentFinding{
		Role:        a.role,
		Summary:     prefix + " LLM summary",
		EvidenceIDs: []string{id},
		Evidence: []common.EvidenceItem{{
			ID: id, SourceType: prefix, Content: prefix + " evidence",
		}},
		Metadata: map[string]any{
			prefix + "_llm_used":        true,
			prefix + "_llm_attempted":   true,
			prefix + "_model":           "test-model",
			prefix + "_fallback_used":   false,
			prefix + "_llm_duration_ms": int64(5),
		},
	}, nil
}

type llmMetadataSynthesizer struct{}

func (llmMetadataSynthesizer) Synthesize(
	_ context.Context,
	input SynthesisInput,
) (agenteino.AgentOutput, error) {
	return agenteino.AgentOutput{
		Conclusions: []agenteino.Conclusion{{
			Text:        "Combined diagnosis",
			EvidenceIDs: []string{"evidence-1", "knowledge-1"},
		}},
		Metadata: map[string]any{
			"synthesis_llm_used":        true,
			"synthesis_llm_attempted":   true,
			"synthesis_model":           "test-model",
			"synthesis_fallback_used":   false,
			"synthesis_llm_duration_ms": int64(5),
			"synthesis_mode":            "llm",
			"fallback_used":             false,
		},
	}, nil
}

func TestOrchestratorAggregatesMultiAgentLLMMetadata(t *testing.T) {
	orchestrator := NewOrchestrator(
		context.Background(),
		llmMetadataTriagePlanner{},
		llmMetadataAnalyzer{role: AgentRoleEvidence},
		llmMetadataAnalyzer{role: AgentRoleKnowledge},
		llmMetadataSynthesizer{},
	)
	result, err := orchestrator.Execute(context.Background(), Input{
		SessionID: "session-1",
		Message:   "Investigate checkout",
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Metadata["multi_agent_llm_used"] != true ||
		result.Metadata["multi_agent_llm_call_count"] != 4 ||
		result.Metadata["triage_model"] != "test-model" ||
		result.Metadata["synthesis_model"] != "test-model" {
		t.Fatalf("Metadata = %#v", result.Metadata)
	}
	roles, ok := result.Metadata["multi_agent_llm_roles"].([]string)
	if !ok || len(roles) != 4 {
		t.Fatalf("multi_agent_llm_roles = %#v", result.Metadata["multi_agent_llm_roles"])
	}
	for _, step := range result.Steps {
		if step.Metadata == nil {
			t.Fatalf("step metadata missing for role %s", step.Role)
		}
	}
}
