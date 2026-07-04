package multiagent

import (
	"context"
	"errors"
	"strings"
	"unicode"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
)

const (
	IncidentHighErrorRate  = "high_error_rate"
	IncidentPaymentTimeout = "payment_timeout"
	IncidentLatency        = "latency"
	IncidentAlert          = "alert"
	IncidentRunbook        = "runbook"
	IncidentUnknown        = "unknown"
)

const maxEvidencePlanSize = 6

var supportedEvidenceSources = []string{
	"metrics",
	"logs",
	"alerts",
	"traces",
	"topology",
	"knowledge",
}

type DeterministicTriageAgent struct {
	defaultService string
}

type LLMTriageAgent struct {
	fallback *DeterministicTriageAgent
	llm      *RoleLLM
}

func NewDeterministicTriageAgent(defaultService string) *DeterministicTriageAgent {
	defaultService = strings.TrimSpace(defaultService)
	if defaultService == "" {
		defaultService = "checkout"
	}
	return &DeterministicTriageAgent{defaultService: defaultService}
}

func NewLLMTriageAgent(defaultService string, llm *RoleLLM) *LLMTriageAgent {
	return &LLMTriageAgent{
		fallback: NewDeterministicTriageAgent(defaultService),
		llm:      llm,
	}
}

func (a *DeterministicTriageAgent) Plan(
	_ context.Context,
	input Input,
) (TriagePlan, error) {
	query := strings.TrimSpace(input.Message)
	language := detectLanguage(query)
	service, serviceCertain := detectService(query)
	limitations := []agenteino.Limitation{}
	if !serviceCertain {
		service = a.defaultService
		limitations = append(limitations, agenteino.Limitation{
			Code: "TRIAGE_SERVICE_UNCERTAIN",
			Message: localizedTriageText(
				language,
				"无法从问题中可靠识别 service；Multi-Agent demo 暂按 checkout 排查。",
				"Service could not be identified reliably; the Multi-Agent demo will investigate checkout.",
			),
		})
	}
	incidentType := detectIncidentType(query)
	evidencePlan := buildEvidencePlan(query, incidentType)
	return TriagePlan{
		Service:      service,
		IncidentType: incidentType,
		EvidencePlan: evidencePlan,
		Query:        query,
		Summary: localizedTriageSummary(
			language,
			service,
			incidentType,
			evidencePlan,
		),
		TimeContext: input.TimeContext,
		Language:    language,
		Limitations: limitations,
		Metadata: map[string]any{
			"triage_mode":            "rule_based",
			"triage_llm_used":        false,
			"triage_llm_attempted":   false,
			"triage_model":           "",
			"triage_fallback_used":   true,
			"triage_llm_duration_ms": int64(0),
			"normalized_query":       query,
			"service_confident":      serviceCertain,
		},
	}, nil
}

func (a *LLMTriageAgent) Plan(ctx context.Context, input Input) (TriagePlan, error) {
	if a == nil || a.fallback == nil {
		return TriagePlan{}, errors.New("triage fallback planner is required")
	}
	if a.llm == nil {
		return a.fallback.Plan(ctx, input)
	}
	// Triage is allowed to improve the investigation plan with an LLM, but not
	// to make root-cause claims. Any unsafe or malformed plan falls back to the
	// deterministic classifier so downstream roles receive a bounded contract.
	rulePlan, err := a.fallback.Plan(ctx, input)
	if err != nil {
		return TriagePlan{}, err
	}
	plan, call, err := a.llm.planTriage(ctx, input, rulePlan)
	if err != nil {
		fallbackPlan := rulePlan
		fallbackPlan.Metadata = mergeTriageMetadata(fallbackPlan.Metadata, map[string]any{
			"triage_mode":            "rule_based",
			"triage_llm_used":        false,
			"triage_llm_attempted":   true,
			"triage_model":           "",
			"triage_fallback_used":   true,
			"triage_llm_duration_ms": call.durationMS,
			"triage_fallback_reason": safeTriageFailureReason(err),
		})
		return fallbackPlan, nil
	}
	plan.Query = strings.TrimSpace(input.Message)
	plan.TimeContext = input.TimeContext
	plan.Metadata = mergeTriageMetadata(plan.Metadata, map[string]any{
		"triage_mode":            "llm",
		"triage_llm_used":        true,
		"triage_llm_attempted":   true,
		"triage_model":           a.llm.modelName,
		"triage_fallback_used":   false,
		"triage_llm_duration_ms": call.durationMS,
	})
	return plan, nil
}

func mergeTriageMetadata(base map[string]any, overrides map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}

func safeTriageFailureReason(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "invalid_json"):
		return "invalid_json"
	case strings.Contains(message, "invalid_output"):
		return "invalid_output"
	case strings.Contains(message, "model_call_failed"):
		return "model_call_failed"
	case strings.Contains(message, "empty_model_response"):
		return "empty_model_response"
	default:
		return "llm_unavailable"
	}
}

func detectLanguage(value string) string {
	for _, current := range value {
		if unicode.Is(unicode.Han, current) {
			return "zh"
		}
	}
	return "en"
}

func detectService(value string) (string, bool) {
	query := strings.ToLower(value)
	for _, service := range []string{
		"checkout",
		"payment",
		"inventory",
		"catalog",
		"redis",
		"mysql",
	} {
		if strings.Contains(query, service) {
			return service, true
		}
	}
	return "", false
}

func detectIncidentType(value string) string {
	query := strings.ToLower(value)
	switch {
	case containsAny(query, "error rate", "errors", "5xx", "错误率", "报错"):
		return IncidentHighErrorRate
	case containsAny(query, "timeout", "deadline", "超时"):
		return IncidentPaymentTimeout
	case containsAny(query, "latency", "slow", "延迟", "慢"):
		return IncidentLatency
	case containsAny(query, "alert", "告警"):
		return IncidentAlert
	case containsAny(query, "runbook", "playbook", "排障手册", "操作手册"):
		return IncidentRunbook
	default:
		return IncidentUnknown
	}
}

func buildEvidencePlan(query string, incidentType string) []string {
	lower := strings.ToLower(query)
	selected := map[string]bool{}
	add := func(values ...string) {
		for _, value := range values {
			selected[value] = true
		}
	}
	switch incidentType {
	case IncidentHighErrorRate:
		add(supportedEvidenceSources...)
	case IncidentPaymentTimeout:
		add("metrics", "logs", "alerts", "traces", "topology", "knowledge")
	case IncidentLatency:
		add("metrics", "logs", "traces", "topology", "knowledge")
	case IncidentAlert:
		add("alerts", "metrics", "logs")
	case IncidentRunbook:
		add("knowledge")
	default:
		add("metrics", "logs", "knowledge")
	}
	if containsAny(lower, "metric", "prometheus", "指标") {
		add("metrics")
	}
	if containsAny(lower, "log", "日志") {
		add("logs")
	}
	if containsAny(lower, "alert", "告警") {
		add("alerts")
	}
	if containsAny(lower, "trace", "span", "链路") {
		add("traces")
	}
	if containsAny(lower, "topology", "dependency", "拓扑", "依赖") {
		add("topology")
	}
	if containsAny(lower, "knowledge", "runbook", "playbook", "知识", "手册") {
		add("knowledge")
	}

	result := make([]string, 0, maxEvidencePlanSize)
	for _, source := range supportedEvidenceSources {
		if selected[source] {
			result = append(result, source)
		}
		if len(result) == maxEvidencePlanSize {
			break
		}
	}
	return result
}

func localizedTriageSummary(
	language string,
	service string,
	incidentType string,
	evidencePlan []string,
) string {
	plan := strings.Join(evidencePlan, ", ")
	if language == "zh" {
		return "分诊结果：service=" + service +
			"，incident_type=" + incidentType +
			"，evidence_plan=[" + plan + "]。"
	}
	return "Triage result: service=" + service +
		", incident_type=" + incidentType +
		", evidence_plan=[" + plan + "]."
}

func localizedTriageText(language string, chinese string, english string) string {
	if language == "zh" {
		return chinese
	}
	return english
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
