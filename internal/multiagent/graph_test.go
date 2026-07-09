package multiagent

import (
	"context"
	"sync"
	"testing"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/diagnosis"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
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

type staticAnalyzer struct {
	finding AgentFinding
}

func (a staticAnalyzer) Analyze(
	context.Context,
	TriagePlan,
) (AgentFinding, error) {
	return a.finding, nil
}

type fakeRoleAwareRetriever struct {
	result retrievalknowledge.RetrievalResult
	err    error
}

func (f fakeRoleAwareRetriever) HybridRetrieve(
	context.Context,
	retrievalknowledge.RetrievalRequest,
) (retrievalknowledge.RetrievalResult, error) {
	return f.result, f.err
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
		if plan.AgentPlan.RoleSkillCards[plan.AgentPlan.SelectedAgents[0]] == "" ||
			plan.AgentPlan.RoleSkillCards[AgentRoleEvidence] == "" ||
			plan.AgentPlan.RoleSkillCards[AgentRoleKnowledge] == "" {
			t.Fatalf("analyzer plan missing role skill cards: %#v", plan.AgentPlan.RoleSkillCards)
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

func TestOrchestratorSelectsKnowledgeAndSynthesisForKnowledgeIntent(t *testing.T) {
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
	result, err := orchestrator.Execute(context.Background(), Input{
		RequestID: "req-knowledge",
		SessionID: "session-knowledge",
		Message:   "find checkout runbook",
		Intent: intent.IntentResult{
			Intent:          intent.IntentKnowledgeQuery,
			Confidence:      0.9,
			SuggestedAgents: []intent.AgentRole{intent.RoleKnowledge, intent.RoleSynthesis},
			Source:          "rule",
		},
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(seen) != 1 || seen[0].Intent.Intent != intent.IntentKnowledgeQuery {
		t.Fatalf("seen = %#v", seen)
	}
	if result.Steps[1].Status != AgentStepSkipped ||
		result.Steps[2].Status != AgentStepCompleted ||
		result.Metadata["intent_type"] != string(intent.IntentKnowledgeQuery) ||
		result.Metadata["dynamic_routing_enabled"] != true {
		t.Fatalf("result = %#v", result)
	}
}

func TestOrchestratorPromotesKnowledgeMemoryMetadata(t *testing.T) {
	orchestrator := NewOrchestrator(
		context.Background(),
		fakeTriagePlanner{},
		staticAnalyzer{finding: AgentFinding{Role: AgentRoleEvidence}},
		staticAnalyzer{finding: AgentFinding{
			Role: AgentRoleKnowledge,
			Metadata: map[string]any{
				"long_term_memory_available":      true,
				"long_term_memory_count":          0,
				"long_term_memory_not_configured": false,
			},
		}},
		fakeSynthesizer{},
	)
	result, err := orchestrator.Execute(context.Background(), Input{
		RequestID: "req-memory-metadata",
		SessionID: "session-memory-metadata",
		Message:   "find checkout memory",
		Intent: intent.IntentResult{
			Intent:     intent.IntentKnowledgeQuery,
			Confidence: 0.9,
			Source:     "rule",
		},
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Metadata["long_term_memory_available"] != true ||
		result.Metadata["long_term_memory_count"] != 0 ||
		result.Metadata["long_term_memory_not_configured"] != false {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
}

func TestOrchestratorStatusSummarySkipsFindingsWithoutMergePanic(t *testing.T) {
	orchestrator := NewOrchestrator(
		context.Background(),
		fakeTriagePlanner{},
		recordingAnalyzer{},
		recordingAnalyzer{},
		fakeSynthesizer{},
	)
	result, err := orchestrator.Execute(context.Background(), Input{
		RequestID: "req-status",
		SessionID: "session-status",
		Message:   "summarize current status",
		Intent: intent.IntentResult{
			Intent:     intent.IntentStatusSummary,
			Confidence: 0.9,
			Source:     "rule",
		},
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for index, role := range []AgentRole{
		AgentRoleTriage,
		AgentRoleEvidence,
		AgentRoleKnowledge,
	} {
		if result.Steps[index].Role != role ||
			result.Steps[index].Status != AgentStepSkipped {
			t.Fatalf("steps = %#v, want first three roles skipped", result.Steps)
		}
	}
	if result.Steps[3].Role != AgentRoleSynthesis ||
		result.Steps[3].Status != AgentStepCompleted {
		t.Fatalf("steps = %#v, want synthesis completed", result.Steps)
	}
}

func TestOrchestratorEvaluatesHypothesesForIncidentTriage(t *testing.T) {
	orchestrator := NewOrchestrator(
		context.Background(),
		fakeTriagePlanner{},
		staticAnalyzer{finding: AgentFinding{
			Role:        AgentRoleEvidence,
			Summary:     "upstream timeout evidence",
			EvidenceIDs: []string{"log-1", "metric-1"},
			Evidence: []common.EvidenceItem{
				{
					ID:         "log-1",
					SourceType: "logs",
					SourceName: "elasticsearch-logs",
					Content:    "checkout upstream dependency timeout with retry amplification",
					Metadata: map[string]any{
						"log_id": "log-1",
						"level":  "error",
					},
				},
				{
					ID:         "metric-1",
					SourceType: "metrics",
					SourceName: "prometheus",
					Content:    "dependency error rate increased for checkout",
					Metadata: map[string]any{
						"metric_name": "dependency_error_rate",
						"value":       0.08,
					},
				},
			},
		}},
		staticAnalyzer{finding: AgentFinding{Role: AgentRoleKnowledge}},
		fakeSynthesizer{},
	)
	result, err := orchestrator.Execute(context.Background(), Input{
		RequestID: "req-hypothesis",
		SessionID: "session-hypothesis",
		Message:   "Why did checkout 5xx error rate increase?",
		Intent: intent.IntentResult{
			Intent:     intent.IntentIncidentTriage,
			Confidence: 0.9,
			Symptom:    "error",
			Source:     "rule",
		},
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	set, ok := result.Metadata["hypotheses"].(diagnosis.HypothesisSet)
	if !ok || len(set.Items) == 0 {
		t.Fatalf("metadata = %#v, want evaluated hypotheses", result.Metadata)
	}
	if set.Items[0].Status != diagnosis.StatusSupported ||
		len(set.Items[0].SupportingEvidence) == 0 {
		t.Fatalf("hypotheses = %#v", set.Items)
	}
}

func TestOrchestratorSelectsEvidenceTriageSynthesisForTraceIntent(t *testing.T) {
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
	result, err := orchestrator.Execute(context.Background(), Input{
		RequestID: "req-trace",
		SessionID: "session-trace",
		Message:   "analyze trace",
		Intent: intent.IntentResult{
			Intent:          intent.IntentTraceAnalysis,
			Confidence:      0.9,
			SuggestedAgents: []intent.AgentRole{intent.RoleEvidence, intent.RoleTriage, intent.RoleSynthesis},
			Source:          "rule",
		},
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(seen) != 1 ||
		seen[0].Intent.Intent != intent.IntentTraceAnalysis ||
		result.Steps[2].Status != AgentStepSkipped {
		t.Fatalf("seen=%#v steps=%#v", seen, result.Steps)
	}
}

func TestRoleAwareRAGSelectsChunksByRole(t *testing.T) {
	context := buildRoleAwareRAGContext(retrievalknowledge.RetrievalResult{
		Chunks: []retrievalknowledge.RetrievedKnowledge{
			{ID: "metrics", Title: "Metrics dashboard", Content: "Prometheus latency panel"},
			{ID: "runbook", Title: "Checkout runbook", Content: "Incident mitigation steps"},
			{ID: "triage", Title: "Diagnosis rule summary", Content: "Triage high error rate"},
		},
		Metadata: map[string]any{"retrieval_mode": "hybrid"},
	})
	if len(context.ChunksByRole[AgentRoleEvidence]) != 1 ||
		len(context.ChunksByRole[AgentRoleKnowledge]) != 1 ||
		len(context.ChunksByRole[AgentRoleTriage]) != 1 ||
		context.SynthesisSummary == "" {
		t.Fatalf("role rag context = %#v", context)
	}
}

func TestOrchestratorPassesRoleAwareRAGToRoles(t *testing.T) {
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
	).WithRoleAwareRAG(fakeRoleAwareRetriever{result: retrievalknowledge.RetrievalResult{
		Chunks: []retrievalknowledge.RetrievedKnowledge{{
			ID: "runbook", ChunkID: "runbook", Title: "Checkout runbook",
			Content: "Inspect payment timeout.", Source: "runbook",
		}},
		Metadata: map[string]any{"retrieval_mode": "hybrid"},
	}})

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
	if len(seen) != 2 ||
		len(seen[0].RoleRAG.ChunksByRole[AgentRoleKnowledge]) != 1 ||
		result.Metadata["role_rag_chunk_count"] == 0 {
		t.Fatalf("seen=%#v metadata=%#v", seen, result.Metadata)
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
