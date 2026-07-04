package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const maxRoleEvidenceItems = 20

type AnalysisChatModel interface {
	Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error)
}

type RoleLLM struct {
	model     AnalysisChatModel
	modelName string
	timeout   time.Duration
}

func NewRoleLLM(
	chatModel AnalysisChatModel,
	modelName string,
	timeout time.Duration,
) (*RoleLLM, error) {
	if chatModel == nil {
		return nil, errors.New("multi-agent analysis model is required")
	}
	if strings.TrimSpace(modelName) == "" {
		return nil, errors.New("multi-agent analysis model name is required")
	}
	if timeout <= 0 {
		return nil, errors.New("multi-agent analysis timeout must be greater than zero")
	}
	return &RoleLLM{
		model:     chatModel,
		modelName: strings.TrimSpace(modelName),
		timeout:   timeout,
	}, nil
}

type evidenceAnalysis struct {
	ObservationSummary      string   `json:"observation_summary"`
	SupportedSignals        []string `json:"supported_signals"`
	SuspectedFailurePattern string   `json:"suspected_failure_pattern"`
	MissingEvidence         []string `json:"missing_evidence"`
	EvidenceIDs             []string `json:"evidence_ids"`
}

type knowledgeAnalysis struct {
	KnowledgeSummary     string   `json:"knowledge_summary"`
	RunbookActions       []string `json:"runbook_supported_actions"`
	HistoricalPatterns   []string `json:"historical_patterns"`
	UnsafeActionsToAvoid []string `json:"unsafe_actions_to_avoid"`
	EvidenceIDs          []string `json:"evidence_ids"`
}

type synthesisDraft struct {
	Conclusions     []synthesisStatement   `json:"conclusions"`
	Inferences      []synthesisStatement   `json:"inferences"`
	Recommendations []synthesisStatement   `json:"recommendations"`
	Limitations     []agenteino.Limitation `json:"limitations"`
}

type synthesisStatement struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type llmCallResult struct {
	durationMS int64
}

func (l *RoleLLM) analyzeEvidence(
	ctx context.Context,
	plan TriagePlan,
	evidence []common.EvidenceItem,
	limitations []agenteino.Limitation,
) (evidenceAnalysis, llmCallResult, error) {
	payload := map[string]any{
		"triage_plan": boundedPlanForPrompt(plan),
		"evidence":    boundedEvidenceForPrompt(evidence),
		"limitations": limitations,
		"language":    plan.Language,
	}
	var output evidenceAnalysis
	call, err := l.callJSON(
		ctx,
		AgentRoleEvidence,
		`You are the Evidence Agent in a service reliability investigation.
Analyze only the supplied tool evidence. Do not invent observations or evidence IDs.
Use the requested language. Keep the analysis concise and return JSON only with:
observation_summary, supported_signals, suspected_failure_pattern, missing_evidence, evidence_ids.
suspected_failure_pattern must remain a hypothesis unless the evidence directly proves it.
Every evidence_id must exactly match an ID in the input.`,
		payload,
		&output,
		func() error {
			if strings.TrimSpace(output.ObservationSummary) == "" {
				return errors.New("evidence LLM returned an empty observation_summary")
			}
			if len(evidence) > 0 && len(output.EvidenceIDs) == 0 {
				return errors.New("evidence LLM returned no evidence IDs")
			}
			return validateExactEvidenceIDs(output.EvidenceIDs, evidence)
		},
	)
	if err != nil {
		return evidenceAnalysis{}, call, err
	}
	return output, call, nil
}

func (l *RoleLLM) analyzeKnowledge(
	ctx context.Context,
	plan TriagePlan,
	evidence []common.EvidenceItem,
	memories []longterm.Memory,
	limitations []agenteino.Limitation,
) (knowledgeAnalysis, llmCallResult, error) {
	payload := map[string]any{
		"triage_plan": boundedPlanForPrompt(plan),
		"knowledge":   boundedEvidenceForPrompt(evidence),
		"memory":      boundedMemoryForPrompt(memories),
		"limitations": limitations,
		"language":    plan.Language,
	}
	var output knowledgeAnalysis
	call, err := l.callJSON(
		ctx,
		AgentRoleKnowledge,
		`You are the Knowledge Agent in a service reliability investigation.
Summarize only the supplied runbook chunks and historical memory.
Historical memory is prior experience, never proof of the current incident.
Do not invent actions or evidence IDs. Use the requested language.
Return JSON only with: knowledge_summary, runbook_supported_actions,
historical_patterns, unsafe_actions_to_avoid, evidence_ids.
Every evidence_id must exactly match an ID in the knowledge input.`,
		payload,
		&output,
		func() error {
			if strings.TrimSpace(output.KnowledgeSummary) == "" {
				return errors.New("knowledge LLM returned an empty knowledge_summary")
			}
			if len(evidence) > 0 && len(output.EvidenceIDs) == 0 {
				return errors.New("knowledge LLM returned no evidence IDs")
			}
			return validateExactEvidenceIDs(output.EvidenceIDs, evidence)
		},
	)
	if err != nil {
		return knowledgeAnalysis{}, call, err
	}
	return output, call, nil
}

func (l *RoleLLM) synthesize(
	ctx context.Context,
	input SynthesisInput,
) (agenteino.AgentOutput, error) {
	payload := map[string]any{
		"triage_plan": boundedPlanForPrompt(input.Plan),
		"evidence_finding": map[string]any{
			"summary":      boundedSummary(input.EvidenceFinding.Summary, 1000),
			"evidence_ids": input.EvidenceFinding.EvidenceIDs,
			"limitations":  input.EvidenceFinding.Limitations,
		},
		"knowledge_finding": map[string]any{
			"summary":      boundedSummary(input.KnowledgeFinding.Summary, 1000),
			"evidence_ids": input.KnowledgeFinding.EvidenceIDs,
			"limitations":  input.KnowledgeFinding.Limitations,
		},
		"evidence":    boundedEvidenceForPrompt(input.Evidence),
		"limitations": input.Limitations,
		"language":    input.Plan.Language,
	}
	var draft synthesisDraft
	call, err := l.callJSON(
		ctx,
		AgentRoleSynthesis,
		`You are the Synthesis Agent for a service reliability investigation.
Produce the final bounded diagnosis using only the supplied evidence and role findings.
Do not claim an observed root cause without direct evidence. Keep hypotheses in inferences.
Every conclusion, inference, and recommendation must cite one or more exact supplied evidence IDs.
Preserve limitations and use the requested language.
Return JSON only with: conclusions, inferences, recommendations, limitations.
Each statement is {"text":"...","evidence_ids":["..."]}; each limitation is
{"code":"...","message":"...","tool":"optional"}.`,
		payload,
		&draft,
		func() error {
			return validateSynthesisDraft(draft, input.Evidence)
		},
	)
	metadata := map[string]any{
		"synthesis_llm_used":        err == nil,
		"synthesis_llm_attempted":   true,
		"synthesis_model":           l.modelName,
		"synthesis_fallback_used":   err != nil,
		"synthesis_llm_duration_ms": call.durationMS,
		"synthesis_mode":            "llm",
	}
	if err != nil {
		return agenteino.AgentOutput{Metadata: metadata}, err
	}
	output := agenteino.AgentOutput{
		Conclusions:     make([]agenteino.Conclusion, 0, len(draft.Conclusions)),
		Inferences:      make([]agenteino.Inference, 0, len(draft.Inferences)),
		Recommendations: make([]agenteino.Recommendation, 0, len(draft.Recommendations)),
		Limitations:     append([]agenteino.Limitation{}, draft.Limitations...),
		Metadata:        metadata,
	}
	for _, item := range draft.Conclusions {
		output.Conclusions = append(output.Conclusions, agenteino.Conclusion{
			Text: item.Text, EvidenceIDs: item.EvidenceIDs,
		})
	}
	for _, item := range draft.Inferences {
		output.Inferences = append(output.Inferences, agenteino.Inference{
			Text: item.Text, EvidenceIDs: item.EvidenceIDs,
		})
	}
	for _, item := range draft.Recommendations {
		output.Recommendations = append(output.Recommendations, agenteino.Recommendation{
			Text: item.Text, EvidenceIDs: item.EvidenceIDs,
		})
	}
	return output, nil
}

func (l *RoleLLM) callJSON(
	ctx context.Context,
	role AgentRole,
	systemPrompt string,
	payload any,
	target any,
	validate func() error,
) (llmCallResult, error) {
	started := time.Now()
	spanName := "multiagent." + string(role) + ".llm_call"
	ctx, span := observability.StartSpan(
		ctx,
		spanName,
		attribute.String("agent.role", string(role)),
		attribute.String("llm.model", l.modelName),
	)
	defer span.End()
	agenteino.EmitStreamEvent(ctx, "agent_llm_started", map[string]any{
		"role":       string(role),
		"agent_role": string(role),
		"model":      l.modelName,
	})

	encoded, err := json.Marshal(payload)
	if err != nil {
		return l.failCall(ctx, span, role, started, "prompt_encode_failed", err)
	}
	callContext, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	response, err := l.model.Generate(callContext, []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(string(encoded)),
	})
	if err != nil {
		return l.failCall(ctx, span, role, started, "model_call_failed", err)
	}
	if response == nil {
		return l.failCall(
			ctx,
			span,
			role,
			started,
			"empty_model_response",
			errors.New("model returned no response"),
		)
	}
	if err := json.Unmarshal([]byte(stripRoleJSONFence(response.Content)), target); err != nil {
		return l.failCall(ctx, span, role, started, "invalid_json", err)
	}
	if validate != nil {
		if err := validate(); err != nil {
			return l.failCall(ctx, span, role, started, "invalid_output", err)
		}
	}
	duration := time.Since(started).Milliseconds()
	span.SetAttributes(
		attribute.Int64("llm.duration_ms", duration),
		attribute.Bool("fallback_used", false),
	)
	agenteino.EmitStreamEvent(ctx, "agent_llm_completed", map[string]any{
		"role":        string(role),
		"agent_role":  string(role),
		"model":       l.modelName,
		"duration_ms": duration,
	})
	return llmCallResult{durationMS: duration}, nil
}

func (l *RoleLLM) failCall(
	ctx context.Context,
	span trace.Span,
	role AgentRole,
	started time.Time,
	reason string,
	err error,
) (llmCallResult, error) {
	duration := time.Since(started).Milliseconds()
	span.SetAttributes(
		attribute.Int64("llm.duration_ms", duration),
		attribute.String("error_code", reason),
		attribute.Bool("fallback_used", true),
	)
	observability.MarkError(span, "multi-agent role LLM analysis failed")
	agenteino.EmitStreamEvent(ctx, "agent_llm_failed", map[string]any{
		"role":        string(role),
		"agent_role":  string(role),
		"model":       l.modelName,
		"error_code":  reason,
		"duration_ms": duration,
	})
	return llmCallResult{durationMS: duration}, fmt.Errorf("%s: %w", reason, err)
}

func validateSynthesisDraft(
	draft synthesisDraft,
	evidence []common.EvidenceItem,
) error {
	if len(evidence) > 0 && len(draft.Conclusions) == 0 {
		return errors.New("synthesis LLM returned no conclusions")
	}
	for _, group := range [][]synthesisStatement{
		draft.Conclusions,
		draft.Inferences,
		draft.Recommendations,
	} {
		for _, statement := range group {
			if strings.TrimSpace(statement.Text) == "" {
				return errors.New("synthesis LLM returned empty statement text")
			}
			if len(statement.EvidenceIDs) == 0 {
				return errors.New("synthesis LLM returned an unbound statement")
			}
			if err := validateExactEvidenceIDs(statement.EvidenceIDs, evidence); err != nil {
				return err
			}
		}
	}
	return nil
}

type LLMSynthesizer struct {
	llm *RoleLLM
}

func NewLLMSynthesizer(llm *RoleLLM) *LLMSynthesizer {
	return &LLMSynthesizer{llm: llm}
}

func (s *LLMSynthesizer) Synthesize(
	ctx context.Context,
	input SynthesisInput,
) (agenteino.AgentOutput, error) {
	if s == nil || s.llm == nil {
		return agenteino.AgentOutput{}, errors.New("multi-agent synthesis LLM unavailable")
	}
	return s.llm.synthesize(ctx, input)
}

func boundedEvidenceForPrompt(evidence []common.EvidenceItem) []map[string]any {
	limit := len(evidence)
	if limit > maxRoleEvidenceItems {
		limit = maxRoleEvidenceItems
	}
	result := make([]map[string]any, 0, limit)
	for _, item := range evidence[:limit] {
		result = append(result, map[string]any{
			"id":          item.ID,
			"source_type": item.SourceType,
			"source_name": item.SourceName,
			"content":     boundedSummary(item.Content, 600),
			"score":       item.Score,
			"metadata":    boundedPromptMetadata(item.Metadata),
		})
	}
	return result
}

func boundedPlanForPrompt(plan TriagePlan) map[string]any {
	return map[string]any{
		"service":       plan.Service,
		"incident_type": plan.IncidentType,
		"evidence_plan": plan.EvidencePlan,
		"query":         boundedSummary(plan.Query, 600),
		"summary":       boundedSummary(plan.Summary, 600),
		"time_context":  plan.TimeContext,
		"language":      plan.Language,
		"limitations":   plan.Limitations,
	}
}

func boundedPromptMetadata(metadata map[string]any) map[string]any {
	allowed := map[string]struct{}{
		"timestamp": {}, "service": {}, "level": {}, "trace_id": {},
		"span_id": {}, "chunk_id": {}, "document_id": {}, "retrieval_mode": {},
		"bm25_score": {}, "vector_score": {}, "rrf_score": {}, "category": {},
		"title": {},
	}
	result := map[string]any{}
	for key, value := range metadata {
		if _, ok := allowed[key]; !ok {
			continue
		}
		switch value.(type) {
		case string, bool, int, int32, int64, float32, float64:
			result[key] = value
		}
	}
	return result
}

func boundedMemoryForPrompt(memories []longterm.Memory) []map[string]any {
	result := make([]map[string]any, 0, len(memories))
	for _, memory := range memories {
		result = append(result, map[string]any{
			"id":      memory.ID,
			"service": memory.Service,
			"title":   boundedSummary(memory.Title, 160),
			"summary": boundedSummary(memory.Summary, 400),
		})
	}
	return result
}

func validateExactEvidenceIDs(ids []string, evidence []common.EvidenceItem) error {
	allowed := evidenceIDSet(evidence)
	for _, id := range ids {
		if _, ok := allowed[strings.TrimSpace(id)]; !ok {
			return fmt.Errorf("LLM cited unknown evidence id %q", id)
		}
	}
	return nil
}

func stripRoleJSONFence(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}
	if newline := strings.IndexByte(content, '\n'); newline >= 0 {
		content = content[newline+1:]
	}
	content = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(content), "```"))
	return content
}

var _ Synthesizer = (*LLMSynthesizer)(nil)
