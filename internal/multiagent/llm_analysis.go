package multiagent

import (
	"bytes"
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
const triageMinRepairBudget = 3 * time.Second

type AnalysisChatModel interface {
	Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error)
}

type RoleLLM struct {
	model     AnalysisChatModel
	modelName string
	timeouts  RoleTimeouts
}

type RoleTimeouts struct {
	TriageLLM    time.Duration
	EvidenceLLM  time.Duration
	KnowledgeLLM time.Duration
	SynthesisLLM time.Duration
}

func (t RoleTimeouts) Normalize(fallback time.Duration) RoleTimeouts {
	if fallback <= 0 {
		fallback = 20 * time.Second
	}
	if t.TriageLLM <= 0 {
		t.TriageLLM = fallback
	}
	if t.EvidenceLLM <= 0 {
		t.EvidenceLLM = fallback
	}
	if t.KnowledgeLLM <= 0 {
		t.KnowledgeLLM = fallback
	}
	if t.SynthesisLLM <= 0 {
		t.SynthesisLLM = fallback
	}
	return t
}

func NewRoleLLM(
	chatModel AnalysisChatModel,
	modelName string,
	timeout time.Duration,
) (*RoleLLM, error) {
	return NewRoleLLMWithTimeouts(chatModel, modelName, RoleTimeouts{}.Normalize(timeout))
}

func NewRoleLLMWithTimeouts(
	chatModel AnalysisChatModel,
	modelName string,
	timeouts RoleTimeouts,
) (*RoleLLM, error) {
	if chatModel == nil {
		return nil, errors.New("multi-agent analysis model is required")
	}
	if strings.TrimSpace(modelName) == "" {
		return nil, errors.New("multi-agent analysis model name is required")
	}
	timeouts = timeouts.Normalize(20 * time.Second)
	if timeouts.TriageLLM <= 0 ||
		timeouts.EvidenceLLM <= 0 ||
		timeouts.KnowledgeLLM <= 0 ||
		timeouts.SynthesisLLM <= 0 {
		return nil, errors.New("multi-agent role timeouts must be greater than zero")
	}
	return &RoleLLM{
		model:     chatModel,
		modelName: strings.TrimSpace(modelName),
		timeouts:  timeouts,
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
	KnowledgeSummary     string     `json:"knowledge_summary"`
	RunbookActions       StringList `json:"runbook_supported_actions"`
	HistoricalPatterns   []string   `json:"historical_patterns"`
	UnsafeActionsToAvoid []string   `json:"unsafe_actions_to_avoid"`
	EvidenceIDs          []string   `json:"evidence_ids"`
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
	SuspectedServices []string           `json:"suspected_services"`
	IncidentType      string             `json:"incident_type"`
	EvidencePlan      []string           `json:"evidence_plan"`
	Hypotheses        []triageHypothesis `json:"hypotheses"`
	Uncertainties     []string           `json:"uncertainties"`
	Language          string             `json:"language"`
}

type triageHypothesis struct {
	Statement            string `json:"statement"`
	RequiresVerification bool   `json:"requires_verification"`
}

type llmCallResult struct {
	durationMS          int64
	timeout             time.Duration
	primaryTimeout      time.Duration
	repairTimeout       time.Duration
	errorCode           string
	errorMessage        string
	retryCount          int
	recoveryReason      string
	primaryMS           int64
	repairMS            int64
	primaryAttempted    bool
	primarySuccess      bool
	primaryErrorCode    string
	primaryErrorMessage string
	repairAttempted     bool
	repairSuccess       bool
	repairErrorCode     string
	repairErrorMessage  string
	repairSkipReason    string
}

func (l *RoleLLM) planTriage(
	ctx context.Context,
	input Input,
	rulePlan TriagePlan,
) (TriagePlan, llmCallResult, error) {
	constraints := BuildTriageConstraints(input, rulePlan)
	payload := map[string]any{
		"user_message":           boundedSummary(input.Message, 1000),
		"time_context":           input.TimeContext,
		"language":               constraints.RequestedLanguage,
		"available_services":     constraints.AllowedServices,
		"detected_services":      constraints.DetectedServices,
		"allowed_incident_types": constraints.AllowedIncidentTypes,
		"allowed_evidence_types": supportedEvidenceSources,
	}
	var output triageLLMOutput
	call, err := l.callJSON(
		ctx,
		AgentRoleTriage,
		triageSystemPrompt(constraints),
		payload,
		&output,
		func() error {
			return validateTriageLLMOutput(output, constraints)
		},
	)
	if err != nil {
		return TriagePlan{}, call, err
	}
	plan := TriagePlan{
		Service:      selectTriageService(output.SuspectedServices, rulePlan.Service),
		IncidentType: normalizeTriageIncidentType(output.IncidentType),
		EvidencePlan: triageEvidencePlan(output, rulePlan.EvidencePlan),
		Query:        strings.TrimSpace(input.Message),
		Summary:      "",
		TimeContext:  input.TimeContext,
		Language:     legacyLanguage(constraints.RequestedLanguage),
		Limitations:  append([]agenteino.Limitation{}, rulePlan.Limitations...),
		Metadata: map[string]any{
			"triage_hypotheses":           boundedTriageHypotheses(output.Hypotheses),
			"triage_uncertainties":        boundedStringSlice(output.Uncertainties, 3, 160),
			"triage_constraints":          constraints,
			"requested_language":          constraints.RequestedLanguage,
			"structured_output_supported": false,
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

func triageSystemPrompt(constraints TriageConstraints) string {
	exampleService := ""
	if len(constraints.DetectedServices) > 0 {
		exampleService = constraints.DetectedServices[0]
	} else if len(constraints.AllowedServices) > 0 {
		exampleService = constraints.AllowedServices[0]
	}
	exampleServices := "[]"
	if exampleService != "" {
		exampleServices = jsonStringList([]string{exampleService})
	}
	return strings.Join([]string{
		"You are the Triage Agent.",
		"Produce a short investigation plan only.",
		"Do not provide final diagnosis, confirmed root cause, remediation, restart, rollback, scaling, or mitigation.",
		"",
		"Allowed services:",
		jsonStringList(constraints.AllowedServices),
		"Detected services in the user request:",
		jsonStringList(constraints.DetectedServices),
		"Allowed incident types:",
		jsonStringList(constraints.AllowedIncidentTypes),
		"Allowed evidence types:",
		jsonStringList(supportedEvidenceSources),
		"",
		"Rules:",
		"- suspected_services may contain only values from Allowed services.",
		"- If Detected services is non-empty, suspected_services should usually equal Detected services and must not add unrelated services.",
		"- If no service is supported, return an empty suspected_services array.",
		"- Do not invent services such as user, system, frontend, browser, host, or customer.",
		"- incident_type must be one of Allowed incident types.",
		"- evidence_plan may contain only values from Allowed evidence types.",
		"- Every hypothesis must be unverified and set requires_verification=true.",
		"- The JSON language field must be the exact literal \"" + constraints.RequestedLanguage + "\". Do not output zh, en, Chinese, or English.",
		"- " + languageInstruction(constraints.RequestedLanguage),
		"- Return JSON only. No Markdown. No explanation outside JSON.",
		"",
		"Required JSON shape:",
		`{"suspected_services":` + exampleServices + `,"incident_type":"unknown","evidence_plan":["metrics","logs","knowledge"],"hypotheses":[{"statement":"` + triageExampleHypothesis(constraints.RequestedLanguage, exampleService) + `","requires_verification":true}],"uncertainties":["` + triageExampleUncertainty(constraints.RequestedLanguage) + `"],"language":"` + constraints.RequestedLanguage + `"}`,
	}, "\n")
}

func triageExampleHypothesis(language string, service string) string {
	if service == "" {
		service = "the suspected service"
	}
	if normalizeRequestedLanguage(language) == "zh-CN" {
		return service + " 可能存在待验证的异常信号，需要继续收集 metrics、logs 和 traces。"
	}
	return service + " may have an unverified incident signal that requires metrics, logs, and traces."
}

func triageExampleUncertainty(language string) string {
	if normalizeRequestedLanguage(language) == "zh-CN" {
		return "根因尚未确认。"
	}
	return "Root cause is not confirmed."
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
		"language":         requestedLanguageForPlan(plan),
		"role_skill_cards": plan.AgentPlan.RoleSkillCards[AgentRoleEvidence],
	}
	var output evidenceAnalysis
	call, err := l.callJSON(
		ctx,
		AgentRoleEvidence,
		`You are the Evidence Agent in a service reliability investigation.
Analyze only the supplied tool evidence. Do not invent observations or evidence IDs.
Follow role_skill_cards to interpret metrics, logs, traces, alerts, and topology.
Use the requested language for all user-visible analysis. Keep service names,
metric names, log fields, trace/span IDs, API paths, commands, config keys,
evidence IDs, and raw excerpts unchanged. Keep the analysis concise and return JSON only with:
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
		"triage_plan":          boundedPlanForPrompt(plan),
		"knowledge":            boundedEvidenceForPrompt(evidence),
		"memory":               boundedMemoryForPrompt(memories),
		"allowed_evidence_ids": evidenceIDsForPrompt(evidence),
		"limitations":          limitations,
		"language":             requestedLanguageForPlan(plan),
		"role_skill_cards":     plan.AgentPlan.RoleSkillCards[AgentRoleKnowledge],
	}
	var output knowledgeAnalysis
	call, err := l.callJSON(
		ctx,
		AgentRoleKnowledge,
		`You are the Knowledge Agent in a service reliability investigation.
Summarize only the supplied runbook chunks and historical memory.
Historical memory is prior experience, never proof of the current incident.
Follow role_skill_cards when separating runbook guidance from current facts.
Do not invent actions or evidence IDs. Use the requested language for all
user-visible guidance. Keep service names, metric names, log fields, trace/span IDs,
API paths, commands, config keys, evidence IDs, and raw excerpts unchanged.
Return JSON only with: knowledge_summary, runbook_supported_actions,
historical_patterns, unsafe_actions_to_avoid, evidence_ids.
runbook_supported_actions must always be a JSON array of strings. Use [] when
there are no runbook-supported actions; never return an empty string or a single
string for this field.
You may cite only IDs from allowed_evidence_ids. Never cite internal memory IDs,
database IDs, document IDs, chunk IDs, or IDs not listed there. If no allowed
evidence supports a claim, return an empty evidence_ids array.
Example:
{"knowledge_summary":"Relevant runbook guidance was found.",
"runbook_supported_actions":["check dependency health","validate timeout budget"],
"historical_patterns":[],"unsafe_actions_to_avoid":[],"evidence_ids":["runbook-1"]}.
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
		"language":         requestedLanguageForPlan(input.Plan),
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
Preserve limitations and use the requested language for all user-visible text.
Keep service names, metric names, log fields, trace/span IDs, API paths, commands,
code, config keys, evidence IDs, and model/product names unchanged.
Return JSON only with: conclusions, inferences, recommendations, limitations.
Each statement is {"text":"...","evidence_ids":["..."]}; each limitation is
{"code":"...","message":"...","tool":"optional"}.`,
		payload,
		&draft,
		func() error {
			return validateSynthesisDraft(draft, input.Evidence)
		},
	)
	metadata := roleLLMMetadata(roleLLMMetadataInput{
		Role:           AgentRoleSynthesis,
		Model:          l.modelName,
		Attempted:      true,
		Success:        err == nil,
		Call:           call,
		Fallback:       err != nil,
		FallbackReason: synthesisFallbackReason(err),
		AnalysisMode:   "llm",
	})
	metadata["synthesis_mode"] = "llm"
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
	timeout := l.timeoutForRole(role)
	callContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	primaryContext, cancelPrimary, primaryTimeout := l.attemptContext(callContext, role, timeout, false)
	primaryStarted := time.Now()
	response, err := l.model.Generate(primaryContext, []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(string(encoded)),
	}, roleModelOptions(role)...)
	cancelPrimary()
	primaryMS := time.Since(primaryStarted).Milliseconds()
	if err != nil {
		return l.failCall(ctx, span, role, started, "model_call_failed", err, 0, primaryMS, 0, primaryTimeout.Milliseconds(), 0)
	}
	if response == nil {
		return l.failCall(
			ctx,
			span,
			role,
			started,
			"empty_model_response",
			errors.New("model returned no response"),
			0,
			primaryMS,
			0,
			primaryTimeout.Milliseconds(),
			0,
		)
	}
	content := stripRoleJSONFence(response.Content)
	retryCount := 0
	recoveryReason := ""
	repairMS := int64(0)
	configuredRepairTimeout := time.Duration(0)
	if err := parseRoleJSON(role, content, target); err != nil {
		if ok, _ := shouldRepairJSON(callContext, role, err); ok {
			retryCount = 1
			recoveryReason = repairReasonFor(role, err)
			repairStarted := time.Now()
			repaired, actualRepairTimeout, repairErr := l.repairJSON(
				callContext,
				role,
				systemPrompt,
				content,
				err,
				payload,
			)
			configuredRepairTimeout = actualRepairTimeout
			repairMS = time.Since(repairStarted).Milliseconds()
			if repairErr == nil {
				content = repaired
				if parseErr := parseRoleJSON(role, content, target); parseErr != nil {
					return l.failCall(
						ctx,
						span,
						role,
						started,
						"invalid_json",
						fmt.Errorf("json repair parse failed: %w; initial parse: %v", parseErr, err),
						int64(retryCount),
						primaryMS,
						repairMS,
						primaryTimeout.Milliseconds(),
						configuredRepairTimeout.Milliseconds(),
					)
				}
			} else {
				return l.failCall(
					ctx,
					span,
					role,
					started,
					"invalid_json",
					fmt.Errorf("json repair failed: %w; initial parse: %v", repairErr, err),
					int64(retryCount),
					primaryMS,
					repairMS,
					primaryTimeout.Milliseconds(),
					configuredRepairTimeout.Milliseconds(),
				)
			}
		} else {
			_, skipReason := shouldRepairJSON(callContext, role, err)
			return l.failCall(ctx, span, role, started, "invalid_json", err, 0, primaryMS, 0, primaryTimeout.Milliseconds(), 0, skipReason)
		}
	}
	if validate != nil {
		if err := validate(); err != nil {
			if ok, _ := shouldRepairJSON(callContext, role, err); retryCount == 0 && ok {
				retryCount = 1
				recoveryReason = repairReasonFor(role, err)
				repairStarted := time.Now()
				repaired, actualRepairTimeout, repairErr := l.repairJSON(
					callContext,
					role,
					systemPrompt,
					content,
					err,
					payload,
				)
				configuredRepairTimeout = actualRepairTimeout
				repairMS = time.Since(repairStarted).Milliseconds()
				if repairErr == nil {
					content = repaired
					if parseErr := parseRoleJSON(role, content, target); parseErr != nil {
						return l.failCall(
							ctx,
							span,
							role,
							started,
							"invalid_json",
							fmt.Errorf("validation repair parse failed: %w; initial validation: %v", parseErr, err),
							int64(retryCount),
							primaryMS,
							repairMS,
							primaryTimeout.Milliseconds(),
							configuredRepairTimeout.Milliseconds(),
						)
					}
					if validationErr := validate(); validationErr == nil {
						goto success
					} else {
						return l.failCall(
							ctx,
							span,
							role,
							started,
							"invalid_output",
							fmt.Errorf("validation repair failed: %w; initial validation: %v", validationErr, err),
							int64(retryCount),
							primaryMS,
							repairMS,
							primaryTimeout.Milliseconds(),
							configuredRepairTimeout.Milliseconds(),
						)
					}
				}
				return l.failCall(
					ctx,
					span,
					role,
					started,
					"invalid_output",
					fmt.Errorf("validation repair request failed: %w; initial validation: %v", repairErr, err),
					int64(retryCount),
					primaryMS,
					repairMS,
					primaryTimeout.Milliseconds(),
					configuredRepairTimeout.Milliseconds(),
				)
			}
			return l.failCall(
				ctx,
				span,
				role,
				started,
				"invalid_output",
				err,
				int64(retryCount),
				primaryMS,
				repairMS,
				primaryTimeout.Milliseconds(),
				0,
				repairSkipReason(callContext, role, err),
			)
		}
	}
success:
	duration := time.Since(started).Milliseconds()
	span.SetAttributes(
		attribute.Int64("llm.duration_ms", duration),
		attribute.Int64("llm.timeout_ms", timeout.Milliseconds()),
		attribute.Int("llm.retry_count", retryCount),
		attribute.String("llm.recovery_reason", recoveryReason),
		attribute.Int64("llm.primary_duration_ms", primaryMS),
		attribute.Int64("llm.repair_duration_ms", repairMS),
		attribute.Int64("llm.primary_timeout_ms", primaryTimeout.Milliseconds()),
		attribute.Int64("llm.repair_timeout_ms", configuredRepairTimeout.Milliseconds()),
		attribute.Bool("llm.used", true),
		attribute.Bool("fallback_used", false),
		attribute.Bool("fallback.used", false),
	)
	agenteino.EmitStreamEvent(ctx, "agent_llm_completed", map[string]any{
		"role":                string(role),
		"agent_role":          string(role),
		"model":               l.modelName,
		"duration_ms":         duration,
		"timeout_ms":          timeout.Milliseconds(),
		"retry_count":         retryCount,
		"primary_duration_ms": primaryMS,
		"repair_duration_ms":  repairMS,
		"primary_timeout_ms":  primaryTimeout.Milliseconds(),
		"repair_timeout_ms":   configuredRepairTimeout.Milliseconds(),
	})
	return llmCallResult{
		durationMS:       duration,
		timeout:          timeout,
		primaryTimeout:   primaryTimeout,
		repairTimeout:    configuredRepairTimeout,
		retryCount:       retryCount,
		recoveryReason:   recoveryReason,
		primaryMS:        primaryMS,
		repairMS:         repairMS,
		primaryAttempted: true,
		primarySuccess:   retryCount == 0,
		repairAttempted:  retryCount > 0,
		repairSuccess:    retryCount > 0,
	}, nil
}

func (l *RoleLLM) attemptContext(
	ctx context.Context,
	role AgentRole,
	total time.Duration,
	repair bool,
) (context.Context, context.CancelFunc, time.Duration) {
	if role != AgentRoleTriage {
		child, cancel := context.WithCancel(ctx)
		return child, cancel, total
	}
	primaryBudget, repairBudget := triageAttemptBudgets(total)
	if repair {
		child, cancel := context.WithTimeout(ctx, repairBudget)
		return child, cancel, repairBudget
	}
	child, cancel := context.WithTimeout(ctx, primaryBudget)
	return child, cancel, primaryBudget
}

func (l *RoleLLM) repairAttemptContext(
	ctx context.Context,
	role AgentRole,
	total time.Duration,
) (context.Context, context.CancelFunc, time.Duration) {
	if role != AgentRoleTriage {
		return l.attemptContext(ctx, role, total, true)
	}
	remaining := remainingContextBudget(ctx)
	if remaining <= 0 {
		child, cancel := context.WithCancel(ctx)
		return child, cancel, 0
	}
	budget := triageMinRepairBudget
	if remaining > 7*time.Second {
		budget = 6 * time.Second
	} else if remaining > triageMinRepairBudget+500*time.Millisecond {
		budget = remaining - 500*time.Millisecond
	} else {
		budget = remaining
	}
	if budget <= 0 {
		budget = remaining
	}
	child, cancel := context.WithTimeout(ctx, budget)
	return child, cancel, budget
}

func triageAttemptBudgets(total time.Duration) (time.Duration, time.Duration) {
	if total <= 0 {
		return 0, 0
	}
	repair := triageMinRepairBudget
	if total < 9*time.Second {
		repair = total / 3
	}
	if repair <= 0 {
		repair = total / 4
	}
	if repair >= total {
		repair = total / 3
	}
	primary := total - repair
	if primary <= 0 {
		primary = total
		repair = 0
	}
	return primary, repair
}

func shouldRepairJSON(ctx context.Context, role AgentRole, err error) (bool, string) {
	if err == nil {
		return false, ""
	}
	if ctx.Err() != nil {
		return false, "parent_context_done"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false, "primary_not_repairable"
	}
	code := classifyLLMError("invalid_output", err)
	if code == "provider_unavailable" || code == "auth_failed" || code == "rate_limited" {
		return false, "primary_not_repairable"
	}
	if role == AgentRoleTriage && remainingContextBudget(ctx) < triageRepairThreshold(ctx) {
		return false, "insufficient_budget"
	}
	return true, ""
}

func repairSkipReason(ctx context.Context, role AgentRole, err error) string {
	_, reason := shouldRepairJSON(ctx, role, err)
	return reason
}

func remainingContextBudget(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return time.Hour
	}
	remaining := time.Until(deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func triageRepairThreshold(ctx context.Context) time.Duration {
	remaining := remainingContextBudget(ctx)
	if remaining < triageMinRepairBudget {
		return remaining / 2
	}
	return triageMinRepairBudget
}

func (l *RoleLLM) repairJSON(
	ctx context.Context,
	role AgentRole,
	systemPrompt string,
	previousOutput string,
	parseErr error,
	payload any,
) (string, time.Duration, error) {
	agenteino.EmitStreamEvent(ctx, "agent_llm_json_repair_started", map[string]any{
		"role":       string(role),
		"agent_role": string(role),
		"model":      l.modelName,
	})
	repairContext, cancel, repairTimeout := l.repairAttemptContext(ctx, role, l.timeoutForRole(role))
	defer cancel()
	response, err := l.model.Generate(repairContext, []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(jsonRepairPrompt(role, previousOutput, parseErr, payload)),
	}, roleModelOptions(role)...)
	if err != nil {
		return "", repairTimeout, err
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return "", repairTimeout, errors.New("json repair returned empty response")
	}
	agenteino.EmitStreamEvent(ctx, "agent_llm_json_repair_completed", map[string]any{
		"role":       string(role),
		"agent_role": string(role),
		"model":      l.modelName,
	})
	return stripRoleJSONFence(response.Content), repairTimeout, nil
}

func roleModelOptions(role AgentRole) []model.Option {
	if role != AgentRoleTriage {
		return nil
	}
	return []model.Option{
		model.WithTemperature(0),
		model.WithMaxTokens(512),
	}
}

func parseRoleJSON(role AgentRole, content string, target any) error {
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	if role == AgentRoleTriage {
		decoder.DisallowUnknownFields()
	}
	return decoder.Decode(target)
}

func jsonRepairPrompt(role AgentRole, previousOutput string, parseErr error, payload any) string {
	lines := []string{
		"Your previous response could not be parsed.",
		"",
		"The field \"runbook_supported_actions\" must be a JSON array of strings.",
		"Use [] when there are no actions.",
		"All list-like fields must be JSON arrays, not plain strings.",
		"",
		"Return corrected JSON only.",
		"Do not use Markdown code fences.",
		"Do not include explanations.",
		"",
		"Previous response:",
		boundedSummary(previousOutput, 4000),
		"",
		"Parse error:",
		boundedSummary(parseErr.Error(), 500),
	}
	message := strings.ToLower(parseErr.Error())
	if role == AgentRoleTriage || strings.Contains(message, "triage") {
		field, reason, excerpt := roleContractRepairDetails(parseErr)
		lines = []string{
			"Your previous response violated the Triage Agent role contract.",
			"",
			"Field: " + field,
			"Reason: " + reason,
			"Excerpt: " + excerpt,
			"",
			"Rewrite it as a short investigation plan using only this JSON shape:",
			`{"suspected_services":["checkout"],"incident_type":"high_error_rate","evidence_plan":["metrics","logs","traces","knowledge"],"hypotheses":[{"statement":"Checkout may have elevated errors that require metrics and log verification.","requires_verification":true}],"uncertainties":["Root cause is not confirmed."],"language":"en"}`,
			"",
			"Allowed suspected_services values:",
			allowedServicesForRepair(payload),
			"If unsure, use checkout. Never use user as a service name.",
			"",
			"Do not include:",
			"- final diagnosis",
			"- root cause conclusion",
			"- remediation",
			"- restart, rollback, scaling, or mitigation actions",
			"",
			"Return valid JSON only.",
			"Do not use Markdown code fences.",
			"Do not include explanations.",
			"Every hypothesis must set requires_verification=true.",
		}
	}
	if strings.Contains(message, "unknown evidence id") {
		lines = []string{
			"Your previous response cited an invalid evidence ID.",
			"",
			"You may cite only these evidence IDs:",
			allowedEvidenceIDsForRepair(payload),
			"",
			"Remove unsupported citations.",
			"Only use an allowed ID when that evidence directly supports the claim.",
			"If no allowed evidence supports the claim, return an empty evidence_ids array.",
			"",
			"Return corrected JSON only.",
			"Do not use Markdown code fences.",
			"Do not include explanations.",
			"",
			"Previous response:",
			boundedSummary(previousOutput, 4000),
			"",
			"Validation error:",
			boundedSummary(parseErr.Error(), 500),
		}
	}
	return strings.Join(lines, "\n")
}

func (l *RoleLLM) failCall(
	ctx context.Context,
	span trace.Span,
	role AgentRole,
	started time.Time,
	reason string,
	err error,
	retryData ...any,
) (llmCallResult, error) {
	duration := time.Since(started).Milliseconds()
	code := classifyLLMError(reason, err)
	message := boundedSummary(err.Error(), 240)
	retries := 0
	primaryMS := int64(0)
	repairMS := int64(0)
	primaryTimeoutMS := int64(0)
	repairTimeoutMS := int64(0)
	repairSkip := ""
	if len(retryData) > 0 {
		retries = int(asInt64(retryData[0]))
	}
	if len(retryData) > 1 {
		primaryMS = asInt64(retryData[1])
	}
	if len(retryData) > 2 {
		repairMS = asInt64(retryData[2])
	}
	if len(retryData) > 3 {
		primaryTimeoutMS = asInt64(retryData[3])
	}
	if len(retryData) > 4 {
		repairTimeoutMS = asInt64(retryData[4])
	}
	if len(retryData) > 5 {
		repairSkip = asString(retryData[5])
	}
	span.SetAttributes(
		attribute.Int64("llm.duration_ms", duration),
		attribute.Int64("llm.timeout_ms", l.timeoutForRole(role).Milliseconds()),
		attribute.Int("llm.retry_count", retries),
		attribute.Int64("llm.primary_duration_ms", primaryMS),
		attribute.Int64("llm.repair_duration_ms", repairMS),
		attribute.Int64("llm.primary_timeout_ms", primaryTimeoutMS),
		attribute.Int64("llm.repair_timeout_ms", repairTimeoutMS),
		attribute.String("llm.repair_skip_reason", repairSkip),
		attribute.String("error_code", code),
		attribute.String("failure_reason", reason),
		attribute.Bool("llm.used", false),
		attribute.Bool("fallback_used", true),
		attribute.Bool("fallback.used", true),
	)
	observability.MarkError(span, "multi-agent role LLM analysis failed")
	agenteino.EmitStreamEvent(ctx, "agent_llm_failed", map[string]any{
		"role":                string(role),
		"agent_role":          string(role),
		"model":               l.modelName,
		"error_code":          code,
		"reason":              reason,
		"duration_ms":         duration,
		"timeout_ms":          l.timeoutForRole(role).Milliseconds(),
		"retry_count":         retries,
		"primary_duration_ms": primaryMS,
		"repair_duration_ms":  repairMS,
		"primary_timeout_ms":  primaryTimeoutMS,
		"repair_timeout_ms":   repairTimeoutMS,
		"repair_skip_reason":  repairSkip,
	})
	return llmCallResult{
		durationMS:          duration,
		timeout:             l.timeoutForRole(role),
		primaryTimeout:      time.Duration(primaryTimeoutMS) * time.Millisecond,
		repairTimeout:       time.Duration(repairTimeoutMS) * time.Millisecond,
		errorCode:           code,
		errorMessage:        message,
		retryCount:          retries,
		primaryMS:           primaryMS,
		repairMS:            repairMS,
		primaryAttempted:    true,
		primarySuccess:      false,
		primaryErrorCode:    code,
		primaryErrorMessage: message,
		repairAttempted:     retries > 0,
		repairSuccess:       false,
		repairErrorCode:     "",
		repairErrorMessage:  "",
		repairSkipReason:    repairSkip,
	}, fmt.Errorf("%s: %w", reason, err)
}

func asInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case time.Duration:
		return typed.Milliseconds()
	default:
		return 0
	}
}

func asString(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func (l *RoleLLM) timeoutForRole(role AgentRole) time.Duration {
	if l == nil {
		return 0
	}
	switch role {
	case AgentRoleTriage:
		return l.timeouts.TriageLLM
	case AgentRoleEvidence:
		return l.timeouts.EvidenceLLM
	case AgentRoleKnowledge:
		return l.timeouts.KnowledgeLLM
	case AgentRoleSynthesis:
		return l.timeouts.SynthesisLLM
	default:
		return l.timeouts.EvidenceLLM
	}
}

func classifyLLMError(reason string, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	switch reason {
	case "invalid_json":
		return "parse_failed"
	case "invalid_output":
		var contractErr RoleContractViolation
		if errors.As(err, &contractErr) {
			return "role_contract_violation"
		}
		var evidenceErr invalidEvidenceIDError
		if errors.As(err, &evidenceErr) {
			return "invalid_evidence_id"
		}
		return "parse_failed"
	case "empty_model_response":
		return "empty_response"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "429") ||
		strings.Contains(message, "rate limit") ||
		strings.Contains(message, "too many requests"):
		return "rate_limited"
	case strings.Contains(message, "401") ||
		strings.Contains(message, "403") ||
		strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "forbidden") ||
		strings.Contains(message, "invalid api key") ||
		strings.Contains(message, "auth"):
		return "auth_failed"
	case strings.Contains(message, "500") ||
		strings.Contains(message, "502") ||
		strings.Contains(message, "503") ||
		strings.Contains(message, "504") ||
		strings.Contains(message, "5xx"):
		return "upstream_5xx"
	case strings.Contains(message, "connection refused") ||
		strings.Contains(message, "no such host") ||
		strings.Contains(message, "provider unavailable") ||
		strings.Contains(message, "unavailable"):
		return "provider_unavailable"
	default:
		return "unknown"
	}
}

func repairReasonFor(role AgentRole, err error) string {
	if err == nil {
		return ""
	}
	var evidenceErr invalidEvidenceIDError
	if errors.As(err, &evidenceErr) {
		return "invalid_evidence_id"
	}
	var contractErr RoleContractViolation
	if errors.As(err, &contractErr) || role == AgentRoleTriage {
		return "primary_output_invalid"
	}
	return "json_parse_failed"
}

type RoleContractViolation struct {
	Field   string
	Index   int
	Reason  string
	Excerpt string
}

func (e RoleContractViolation) Error() string {
	field := strings.TrimSpace(e.Field)
	if field == "" {
		field = "triage"
	}
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		reason = "role contract violation"
	}
	if e.Index >= 0 {
		field = fmt.Sprintf("%s[%d]", field, e.Index)
	}
	if e.Excerpt == "" {
		return fmt.Sprintf("%s: %s", field, reason)
	}
	return fmt.Sprintf("%s: %s: %s", field, reason, boundedSummary(e.Excerpt, 120))
}

func roleContractRepairDetails(err error) (string, string, string) {
	var violation RoleContractViolation
	if errors.As(err, &violation) {
		field := violation.Field
		if violation.Index >= 0 {
			field = fmt.Sprintf("%s[%d]", field, violation.Index)
		}
		return nonEmpty(field, "triage"),
			nonEmpty(violation.Reason, "role contract violation"),
			boundedSummary(violation.Excerpt, 160)
	}
	return "triage", boundedSummary(err.Error(), 180), ""
}

func nonEmpty(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

type invalidEvidenceIDError struct {
	ID string
}

func (e invalidEvidenceIDError) Error() string {
	return fmt.Sprintf("LLM cited unknown evidence id %q", e.ID)
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

func validateTriageLLMOutput(output triageLLMOutput, constraints TriageConstraints) error {
	if len(output.SuspectedServices) > 3 {
		return RoleContractViolation{
			Field:   "suspected_services",
			Index:   -1,
			Reason:  "too many suspected services",
			Excerpt: strings.Join(output.SuspectedServices, ", "),
		}
	}
	for index, service := range output.SuspectedServices {
		if !stringInSet(strings.ToLower(strings.TrimSpace(service)), constraints.AllowedServices) {
			return RoleContractViolation{
				Field:   "suspected_services",
				Index:   index,
				Reason:  "unsupported service " + fmt.Sprintf("%q", service) + "; allowed values: " + jsonStringList(constraints.AllowedServices),
				Excerpt: service,
			}
		}
		if len([]rune(service)) > 40 {
			return RoleContractViolation{
				Field:   "suspected_services",
				Index:   index,
				Reason:  "service name is too long",
				Excerpt: service,
			}
		}
	}
	incidentType := normalizeTriageIncidentType(output.IncidentType)
	if incidentType == "" || !stringInSet(incidentType, constraints.AllowedIncidentTypes) {
		return RoleContractViolation{
			Field:   "incident_type",
			Index:   -1,
			Reason:  "unsupported incident_type; allowed values: " + jsonStringList(constraints.AllowedIncidentTypes),
			Excerpt: output.IncidentType,
		}
	}
	if normalizeRequestedLanguage(output.Language) != constraints.RequestedLanguage {
		return RoleContractViolation{
			Field:   "language",
			Index:   -1,
			Reason:  "language must equal " + constraints.RequestedLanguage,
			Excerpt: output.Language,
		}
	}
	if len(triageEvidencePlan(output, nil)) == 0 {
		return errors.New("triage LLM returned empty or unsupported evidence_plan")
	}
	if len(output.EvidencePlan) > maxEvidencePlanSize {
		return RoleContractViolation{
			Field:   "evidence_plan",
			Index:   -1,
			Reason:  "too many evidence sources",
			Excerpt: strings.Join(output.EvidencePlan, ", "),
		}
	}
	if len(output.Hypotheses) == 0 {
		return errors.New("triage LLM returned no hypotheses")
	}
	if len(output.Hypotheses) > 3 {
		return RoleContractViolation{
			Field:   "hypotheses",
			Index:   -1,
			Reason:  "too many hypotheses",
			Excerpt: fmt.Sprintf("%d hypotheses", len(output.Hypotheses)),
		}
	}
	for index, hypothesis := range output.Hypotheses {
		statement := strings.TrimSpace(hypothesis.Statement)
		if statement == "" {
			return errors.New("triage LLM returned empty hypothesis statement")
		}
		if len([]rune(statement)) > 180 {
			return RoleContractViolation{
				Field:   "hypotheses.statement",
				Index:   index,
				Reason:  "hypothesis is too long",
				Excerpt: statement,
			}
		}
		if !hypothesis.RequiresVerification {
			return RoleContractViolation{
				Field:   "hypotheses.requires_verification",
				Index:   index,
				Reason:  "requires_verification must be true",
				Excerpt: statement,
			}
		}
		if containsFinalDiagnosisClaim(statement) {
			return RoleContractViolation{
				Field:   "hypotheses.statement",
				Index:   index,
				Reason:  "stated a final diagnosis or operational action",
				Excerpt: statement,
			}
		}
	}
	if len(output.Uncertainties) > 3 {
		return RoleContractViolation{
			Field:   "uncertainties",
			Index:   -1,
			Reason:  "too many uncertainties",
			Excerpt: strings.Join(output.Uncertainties, ", "),
		}
	}
	for index, value := range output.Uncertainties {
		if containsFinalDiagnosisClaim(value) {
			return RoleContractViolation{
				Field:   "uncertainties",
				Index:   index,
				Reason:  "stated a final diagnosis or operational action",
				Excerpt: value,
			}
		}
		if len([]rune(value)) > 160 {
			return RoleContractViolation{
				Field:   "uncertainties",
				Index:   index,
				Reason:  "uncertainty is too long",
				Excerpt: value,
			}
		}
	}
	return nil
}

func stringInSet(value string, allowed []string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == strings.ToLower(strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
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

func selectTriageService(values []string, fallback string) string {
	for _, value := range values {
		if isSupportedService(value) {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return strings.TrimSpace(fallback)
}

func triageEvidencePlan(output triageLLMOutput, fallback []string) []string {
	normalized := normalizeEvidencePlan(output.EvidencePlan)
	if len(normalized) == 0 {
		return normalizeEvidencePlan(fallback)
	}
	return normalizeEvidencePlan(append(normalized, fallback...))
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

func boundedTriageHypotheses(values []triageHypothesis) []map[string]any {
	limit := len(values)
	if limit > 3 {
		limit = 3
	}
	result := make([]map[string]any, 0, limit)
	for _, value := range values[:limit] {
		statement := strings.TrimSpace(value.Statement)
		if statement == "" {
			continue
		}
		result = append(result, map[string]any{
			"statement":             boundedSummary(statement, 180),
			"requires_verification": value.RequiresVerification,
		})
	}
	return result
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
	if text == "" {
		return false
	}
	if containsAny(
		text,
		"not confirmed",
		"not yet confirmed",
		"unconfirmed",
		"requires verification",
		"needs verification",
		"may be",
		"might be",
		"could be",
		"待验证",
		"未确认",
		"尚未确认",
		"需要验证",
		"可能",
	) && !containsOperationalAction(text) {
		return false
	}
	return containsAny(
		text,
		"root cause is",
		"confirmed root cause",
		"definitive root cause",
		"final diagnosis",
		"final conclusion",
		"remediation",
		"restart",
		"rollback",
		"scale up",
		"mitigation",
		"根因是",
		"最终根因",
		"已确认根因",
		"确定是",
		"最终诊断",
		"最终结论",
		"重启",
		"回滚",
		"扩容",
		"缓解",
	)
}

func containsOperationalAction(text string) bool {
	return containsAny(
		text,
		"remediation",
		"restart",
		"rollback",
		"scale up",
		"scale down",
		"failover",
		"mitigation",
		"重启",
		"回滚",
		"扩容",
		"缩容",
		"切流",
		"故障转移",
		"缓解",
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

func evidenceIDsForPrompt(evidence []common.EvidenceItem) []string {
	result := make([]string, 0, len(evidence))
	for _, item := range evidence {
		if id := strings.TrimSpace(item.ID); id != "" {
			result = append(result, id)
		}
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
		"language":         requestedLanguageForPlan(plan),
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

func requestedLanguageForPlan(plan TriagePlan) string {
	if language := normalizeRequestedLanguage(metadataString(plan.Metadata, "requested_language")); language != "" {
		return language
	}
	if language := normalizeRequestedLanguage(plan.Language); language != "" {
		return language
	}
	if plan.Language == "zh" {
		return "zh-CN"
	}
	return "en-US"
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
			return invalidEvidenceIDError{ID: strings.TrimSpace(id)}
		}
	}
	return nil
}

func allowedEvidenceIDsForRepair(payload any) string {
	if values, ok := payload.(map[string]any); ok {
		if ids, ok := values["allowed_evidence_ids"].([]string); ok {
			encoded, err := json.Marshal(ids)
			if err == nil {
				return string(encoded)
			}
		}
	}
	return "[]"
}

func allowedServicesForRepair(payload any) string {
	if values, ok := payload.(map[string]any); ok {
		switch raw := values["available_services"].(type) {
		case []string:
			encoded, err := json.Marshal(raw)
			if err == nil {
				return string(encoded)
			}
		case []any:
			services := make([]string, 0, len(raw))
			for _, item := range raw {
				service, ok := item.(string)
				if !ok || strings.TrimSpace(service) == "" {
					continue
				}
				services = append(services, strings.TrimSpace(service))
			}
			encoded, err := json.Marshal(services)
			if err == nil {
				return string(encoded)
			}
		}
	}
	encoded, err := json.Marshal(supportedServices())
	if err != nil {
		return `["checkout"]`
	}
	return string(encoded)
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
