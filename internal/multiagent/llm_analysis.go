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
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
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

type triageLLMOutput struct {
	Service          string   `json:"service"`
	IncidentType     string   `json:"incident_type"`
	SeverityHint     string   `json:"severity_hint"`
	EvidencePlan     []string `json:"evidence_plan"`
	Language         string   `json:"language"`
	TimeWindowReason string   `json:"time_window_reason"`
	TriageSummary    string   `json:"triage_summary"`
	Constraints      []string `json:"constraints"`
}

type llmCallResult struct {
	durationMS int64
}

func (l *RoleLLM) planTriage(
	ctx context.Context,
	input Input,
	rulePlan TriagePlan,
) (TriagePlan, llmCallResult, error) {
	payload := map[string]any{
		"user_message":           boundedSummary(input.Message, 1000),
		"time_context":           input.TimeContext,
		"language":               rulePlan.Language,
		"available_services":     supportedServices(),
		"allowed_evidence_types": supportedEvidenceSources,
		"rule_based_candidate":   boundedPlanForPrompt(rulePlan),
		"role_rag_context":       input.Metadata["role_rag_context"],
		"role_skill_cards":       input.Metadata["role_skill_cards"],
		"session_context":        boundedSessionContextForPrompt(input.Metadata["session_context"]),
		"constraints": []string{
			"Triage must not make final root-cause claims.",
			"Triage must only produce a bounded investigation plan.",
			"Triage must choose evidence types from the allowed list.",
			"Triage must output JSON only.",
			"Do not expose private chain-of-thought.",
		},
	}
	var output triageLLMOutput
	call, err := l.callJSON(
		ctx,
		AgentRoleTriage,
		`You are the Triage Agent in a service reliability investigation.
Produce only a bounded investigation plan, not a final diagnosis.
Do not claim a root cause, do not invent evidence, and do not expose chain-of-thought.
Choose evidence_plan values only from the allowed_evidence_types list.
Follow role_skill_cards as bounded diagnostic guidance, not as an execution engine.
Use the requested language for triage_summary and constraints.
Return JSON only with:
service, incident_type, severity_hint, evidence_plan, language,
time_window_reason, triage_summary, constraints.
incident_type must be one of:
high_error_rate, latency, timeout, dependency_failure, unknown.`,
		payload,
		&output,
		func() error {
			return validateTriageLLMOutput(output)
		},
	)
	if err != nil {
		return TriagePlan{}, call, err
	}
	plan := TriagePlan{
		Service:      strings.TrimSpace(output.Service),
		IncidentType: normalizeTriageIncidentType(output.IncidentType),
		EvidencePlan: normalizeEvidencePlan(output.EvidencePlan),
		Query:        strings.TrimSpace(input.Message),
		Summary:      strings.TrimSpace(output.TriageSummary),
		TimeContext:  input.TimeContext,
		Language:     normalizeTriageLanguage(output.Language, rulePlan.Language),
		Limitations:  append([]agenteino.Limitation{}, rulePlan.Limitations...),
		Metadata: map[string]any{
			"triage_severity_hint":      normalizeSeverityHint(output.SeverityHint),
			"triage_time_window_reason": boundedSummary(output.TimeWindowReason, 240),
			"triage_constraints":        boundedStringSlice(output.Constraints, 5, 160),
		},
	}
	if plan.Summary == "" {
		plan.Summary = localizedTriageSummary(
			plan.Language,
			plan.Service,
			plan.IncidentType,
			plan.EvidencePlan,
		)
	}
	return plan, call, nil
}

func (l *RoleLLM) analyzeEvidence(
	ctx context.Context,
	plan TriagePlan,
	evidence []common.EvidenceItem,
	limitations []agenteino.Limitation,
) (evidenceAnalysis, llmCallResult, error) {
	payload := map[string]any{
		"triage_plan":      boundedPlanForPrompt(plan),
		"evidence":         boundedEvidenceForPrompt(evidence),
		"limitations":      limitations,
		"language":         plan.Language,
		"role_skill_cards": plan.AgentPlan.RoleSkillCards[AgentRoleEvidence],
	}
	var output evidenceAnalysis
	call, err := l.callJSON(
		ctx,
		AgentRoleEvidence,
		`You are the Evidence Agent in a service reliability investigation.
Analyze only the supplied tool evidence. Do not invent observations or evidence IDs.
Follow role_skill_cards to interpret metrics, logs, traces, alerts, and topology.
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
		"triage_plan":      boundedPlanForPrompt(plan),
		"knowledge":        boundedEvidenceForPrompt(evidence),
		"memory":           boundedMemoryForPrompt(memories),
		"limitations":      limitations,
		"language":         plan.Language,
		"role_skill_cards": plan.AgentPlan.RoleSkillCards[AgentRoleKnowledge],
	}
	var output knowledgeAnalysis
	call, err := l.callJSON(
		ctx,
		AgentRoleKnowledge,
		`You are the Knowledge Agent in a service reliability investigation.
Summarize only the supplied runbook chunks and historical memory.
Historical memory is prior experience, never proof of the current incident.
Follow role_skill_cards when separating runbook guidance from current facts.
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
		"evidence":         boundedEvidenceForPrompt(input.Evidence),
		"hypotheses":       input.Hypotheses,
		"limitations":      input.Limitations,
		"language":         input.Plan.Language,
		"role_skill_cards": input.Plan.AgentPlan.RoleSkillCards[AgentRoleSynthesis],
	}
	var draft synthesisDraft
	call, err := l.callJSON(
		ctx,
		AgentRoleSynthesis,
		`You are the Synthesis Agent for a service reliability investigation.
Produce the final bounded diagnosis using only the supplied evidence and role findings.
Follow role_skill_cards: consume existing findings and do not request or invent new evidence.
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
	// Role prompts intentionally cross a JSON boundary. Parsing and validation
	// are kept here so every LLM-backed role has the same safe fallback trigger
	// for malformed output, invented evidence IDs, or contract drift.
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
		attribute.Bool("llm.used", true),
		attribute.Bool("fallback_used", false),
		attribute.Bool("fallback.used", false),
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
		attribute.Bool("llm.used", false),
		attribute.Bool("fallback_used", true),
		attribute.Bool("fallback.used", true),
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

func validateTriageLLMOutput(output triageLLMOutput) error {
	if strings.TrimSpace(output.Service) == "" {
		return errors.New("triage LLM returned empty service")
	}
	if !isSupportedService(output.Service) {
		return fmt.Errorf("triage LLM returned unsupported service %q", output.Service)
	}
	incidentType := normalizeTriageIncidentType(output.IncidentType)
	if incidentType == "" {
		return fmt.Errorf("triage LLM returned invalid incident_type %q", output.IncidentType)
	}
	if len(normalizeEvidencePlan(output.EvidencePlan)) == 0 {
		return errors.New("triage LLM returned empty or unsupported evidence_plan")
	}
	if summary := strings.TrimSpace(output.TriageSummary); summary == "" {
		return errors.New("triage LLM returned empty triage_summary")
	} else if containsFinalDiagnosisClaim(summary) {
		return errors.New("triage LLM returned final diagnosis instead of triage plan")
	}
	for _, constraint := range output.Constraints {
		if containsFinalDiagnosisClaim(constraint) {
			return errors.New("triage LLM constraints contain final diagnosis language")
		}
	}
	return nil
}

func normalizeTriageIncidentType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case IncidentHighErrorRate:
		return IncidentHighErrorRate
	case IncidentLatency:
		return IncidentLatency
	case "timeout", IncidentPaymentTimeout:
		return "timeout"
	case "dependency_failure":
		return "dependency_failure"
	case IncidentUnknown, "":
		return IncidentUnknown
	default:
		return ""
	}
}

func normalizeEvidencePlan(values []string) []string {
	allowed := map[string]bool{}
	for _, source := range supportedEvidenceSources {
		allowed[source] = true
	}
	seen := map[string]bool{}
	result := make([]string, 0, maxEvidencePlanSize)
	for _, value := range values {
		source := strings.ToLower(strings.TrimSpace(value))
		if !allowed[source] || seen[source] {
			continue
		}
		seen[source] = true
		result = append(result, source)
		if len(result) == maxEvidencePlanSize {
			break
		}
	}
	return result
}

func normalizeTriageLanguage(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "zh", "en":
		return strings.ToLower(strings.TrimSpace(value))
	}
	if fallback == "zh" {
		return "zh"
	}
	return "en"
}

func normalizeSeverityHint(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "info", "warning", "critical":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func supportedServices() []string {
	return []string{"checkout", "payment", "inventory", "catalog", "redis", "mysql"}
}

func isSupportedService(value string) bool {
	service := strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range supportedServices() {
		if service == candidate {
			return true
		}
	}
	return false
}

func containsFinalDiagnosisClaim(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	return containsAny(
		text,
		"root cause is",
		"confirmed root cause",
		"definitive root cause",
		"final diagnosis",
		"根因是",
		"最终根因",
		"已确认根因",
		"确定是",
	)
}

func boundedStringSlice(values []string, limit int, maxLength int) []string {
	if limit <= 0 {
		return nil
	}
	result := make([]string, 0, limit)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, boundedSummary(value, maxLength))
		if len(result) == limit {
			break
		}
	}
	return result
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
		"service":          plan.Service,
		"incident_type":    plan.IncidentType,
		"evidence_plan":    plan.EvidencePlan,
		"query":            boundedSummary(plan.Query, 600),
		"summary":          boundedSummary(plan.Summary, 600),
		"time_context":     plan.TimeContext,
		"language":         plan.Language,
		"limitations":      plan.Limitations,
		"role_skill_cards": plan.AgentPlan.RoleSkillCards,
		"session_context":  boundedSessionContextForPrompt(plan.Metadata["session_context"]),
		"role_rag": map[string]any{
			"triage":            roleRAGPromptChunks(plan.RoleRAG.ChunksByRole[AgentRoleTriage]),
			"evidence":          roleRAGPromptChunks(plan.RoleRAG.ChunksByRole[AgentRoleEvidence]),
			"knowledge":         roleRAGPromptChunks(plan.RoleRAG.ChunksByRole[AgentRoleKnowledge]),
			"synthesis_summary": boundedSummary(plan.RoleRAG.SynthesisSummary, 600),
			"metadata":          boundedPromptMetadata(plan.RoleRAG.Metadata),
		},
	}
}

func boundedPromptMetadata(metadata map[string]any) map[string]any {
	allowed := map[string]struct{}{
		"timestamp": {}, "service": {}, "level": {}, "trace_id": {},
		"span_id": {}, "chunk_id": {}, "document_id": {}, "retrieval_mode": {},
		"bm25_score": {}, "vector_score": {}, "rrf_score": {}, "category": {},
		"title": {}, "role_rag_used": {}, "role_rag_chunk_count": {},
		"retrieval_latency_ms": {}, "fallback_to_bm25": {}, "vector_enabled": {},
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

func boundedSessionContextForPrompt(value any) map[string]any {
	contextMap, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	result := map[string]any{}
	if summary, ok := contextMap["summary"].(session.Summary); ok {
		result["summary"] = map[string]any{
			"content":         boundedSummary(summary.Content, 500),
			"goal":            boundedSummary(summary.Goal, 180),
			"confirmed_facts": boundedStringSlice(summary.ConfirmedFacts, 5, 160),
			"open_questions":  boundedStringSlice(summary.OpenQuestions, 5, 160),
			"summary_version": summary.Version,
			"important_entities": boundedStringSlice(
				summary.ImportantEntities,
				8,
				80,
			),
		}
	}
	if count, ok := contextMap["recent_message_count"].(int); ok {
		result["recent_message_count"] = count
	}
	if version, ok := contextMap["summary_version"].(int64); ok {
		result["summary_version"] = version
	}
	if messages, ok := contextMap["recent_messages"].([]session.Message); ok {
		limit := len(messages)
		if limit > 4 {
			limit = 4
		}
		recent := make([]map[string]any, 0, limit)
		for _, message := range messages[:limit] {
			recent = append(recent, map[string]any{
				"role":    message.Role,
				"content": boundedSummary(message.Content, 300),
			})
		}
		result["recent_messages"] = recent
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
