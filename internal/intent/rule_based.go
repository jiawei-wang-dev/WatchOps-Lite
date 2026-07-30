package intent

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

var (
	traceIDPattern          = regexp.MustCompile(`(?i)\b[0-9a-f]{16}(?:[0-9a-f]{16})?\b`)
	servicePattern          = regexp.MustCompile(`(?i)\b[a-z][a-z0-9_.-]*(?:-service|_service|\.service|service|gateway)\b`)
	timeRangePattern        = regexp.MustCompile(`(?i)(?:last|最近|过去)\s*(\d{1,3})\s*(?:m|min|minute|minutes|分钟)`)
	chineseTenMinutePattern = regexp.MustCompile(`(?:最近|过去)\s*十\s*分钟`)
	commonServicePattern    = regexp.MustCompile(`(?i)\b(checkout|payment|orders?|cart|catalog|frontend|backend|api|auth|inventory|shipping)\b`)
)

type RuleBasedRecognizer struct{}

func NewRuleBasedRecognizer() *RuleBasedRecognizer {
	return &RuleBasedRecognizer{}
}

func (r *RuleBasedRecognizer) Recognize(
	ctx context.Context,
	input RecognitionInput,
) (IntentResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"intent.rule",
		attribute.Int("message_length", len(input.Message)),
	)
	defer span.End()

	message := strings.TrimSpace(input.Message)
	traceID := detectTraceID(message)
	intentType, confidence, reason := classifyIntentByKeywords(message, traceID)
	service := detectService(message)
	timeRange := detectTimeRange(message, input.Now)
	contextApplied := false
	if ordinal := referencedCandidateIndex(message); ordinal >= 0 {
		if len(input.Focus.Candidates) > ordinal {
			service = input.Focus.Candidates[ordinal]
			intentType = validContextIntent(input.Focus.LastIntent)
			confidence = 0.84
			reason = "resolved ordinal reference from session focus"
			contextApplied = true
		} else {
			confidence = 0.3
			reason = "ordinal reference has no session candidates"
		}
	} else if isEllipticalContinuation(message, intentType) && input.Focus.Available {
		intentType = validContextIntent(input.Focus.LastIntent)
		confidence = 0.78
		reason = "resolved elliptical continuation from session focus"
		contextApplied = true
	}
	if service == "" && contextApplied {
		service = strings.TrimSpace(input.Focus.KnownSlots["service"])
	}
	if timeRange == nil && contextApplied {
		timeRange = parseFocusTimeRange(input.Focus.KnownSlots["time_range"])
	}
	result := IntentResult{
		Intent:          intentType,
		Confidence:      confidence,
		Reason:          reason,
		Service:         service,
		TimeRange:       timeRange,
		TraceID:         traceID,
		Symptom:         detectSymptom(message),
		Keywords:        keywordCandidates(message),
		SuggestedTools:  suggestToolsForIntent(intentType, traceID),
		SuggestedAgents: suggestAgentsForIntent(intentType),
		RAGHints:        buildRAGHints(intentType, message),
		Source:          "rule",
		Metadata: map[string]any{
			"rule_based":            true,
			"session_focus_applied": contextApplied,
		},
	}
	normalized := Normalize(result)
	span.SetAttributes(
		attribute.String("intent.type", string(normalized.Intent)),
		attribute.Float64("intent.confidence", normalized.Confidence),
		attribute.Int("selected_tools_count", len(normalized.SuggestedTools)),
		attribute.Int("selected_agents_count", len(normalized.SuggestedAgents)),
	)
	return normalized, nil
}

func detectTraceID(message string) string {
	match := traceIDPattern.FindString(message)
	return strings.ToLower(strings.TrimSpace(match))
}

func detectService(message string) string {
	match := servicePattern.FindString(message)
	if match == "" {
		match = commonServicePattern.FindString(message)
	}
	if match == "" {
		return ""
	}
	return strings.Trim(match, ".,;:()[]{}")
}

func detectTimeRange(message string, now time.Time) *TimeRangeHint {
	match := timeRangePattern.FindStringSubmatch(message)
	if len(match) >= 2 {
		return &TimeRangeHint{Relative: "last_" + match[1] + "_minutes"}
	}
	if chineseTenMinutePattern.MatchString(message) {
		return &TimeRangeHint{Relative: "last_10_minutes"}
	}
	lower := strings.ToLower(message)
	type candidate struct {
		index int
		value string
	}
	candidates := []candidate{
		{strings.LastIndex(lower, "昨天"), "yesterday"},
		{strings.LastIndex(lower, "yesterday"), "yesterday"},
		{strings.LastIndex(lower, "今天"), "today"},
		{strings.LastIndex(lower, "today"), "today"},
		{strings.LastIndex(lower, "上周"), "last_week"},
		{strings.LastIndex(lower, "last week"), "last_week"},
	}
	selected := candidate{index: -1}
	for _, current := range candidates {
		if current.index > selected.index {
			selected = current
		}
	}
	if selected.index >= 0 {
		return &TimeRangeHint{Relative: selected.value}
	}
	return nil
}

func isSecondReference(message string) bool {
	return referencedCandidateIndex(message) == 1
}

func referencedCandidateIndex(message string) int {
	value := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(value, "第一个"), strings.Contains(value, "第1个"),
		strings.Contains(value, "the first one"), value == "first":
		return 0
	case strings.Contains(value, "第二个"), strings.Contains(value, "第2个"),
		strings.Contains(value, "the second one"), value == "second":
		return 1
	default:
		return -1
	}
}

func isContextReference(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	return referencedCandidateIndex(message) >= 0 ||
		containsAny(lower, "就刚才那个", "刚才那个", "时间不变", "same time")
}

func isEllipticalContinuation(message string, classified IntentType) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if classified != IntentGeneralChat {
		return false
	}
	return isContextReference(message) ||
		containsAny(lower, "换成", "改成", "生产环境呢") ||
		commonServicePattern.MatchString(lower)
}

func validContextIntent(value string) IntentType {
	candidate := IntentType(strings.TrimSpace(value))
	if normalizeIntent(candidate) == IntentGeneralChat && candidate != IntentGeneralChat {
		return IntentIncidentTriage
	}
	return normalizeIntent(candidate)
}

func parseFocusTimeRange(value string) *TimeRangeHint {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &TimeRangeHint{Relative: value}
}

func detectSymptom(message string) string {
	lower := strings.ToLower(message)
	switch {
	case containsAny(lower, "timeout", "超时", "deadline"):
		return "timeout"
	case containsAny(lower, "latency", "slow", "p95", "耗时", "慢"):
		return "latency"
	case containsAny(lower, "error", "5xx", "500", "fail", "失败", "报错", "错误率", "錯誤率", "异常"):
		return "error"
	case containsAny(lower, "panic", "exception", "stack"):
		return "exception"
	default:
		return ""
	}
}

func classifyIntentByKeywords(message string, traceID string) (IntentType, float64, string) {
	lower := strings.ToLower(message)
	if containsAny(lower, "先看日志", "先查日志", "只看日志", "只查日志", "logs first", "log first", "prefer logs", "only logs") {
		return IntentLogsQuery, 0.88, "explicit log preference detected"
	}
	if containsAny(lower, "trace", "span", "链路", "慢调用") &&
		containsAny(lower, "metric", "metrics", "qps", "error rate", "错误率", "錯誤率", "latency", "p95", "指标", "log", "日志") &&
		containsAny(lower, "建议", "advice", "recommend", "结合", "综合") {
		return IntentIncidentTriage, 0.88, "composite diagnostic request detected"
	}
	if traceID != "" || containsAny(lower, "trace", "span", "链路", "慢调用") {
		return IntentTraceAnalysis, 0.9, "trace signal detected"
	}
	if containsAny(lower, "总结", "汇总", "当前状态", "进展", "status summary", "summarize") {
		return IntentStatusSummary, 0.82, "status summary signal detected"
	}
	if containsAny(lower, "缓解", "止损", "mitigate", "mitigation", "remediation advice") {
		return IntentMitigation, 0.84, "mitigation advice signal detected"
	}
	if containsAny(lower, "没报错", "没有报错", "no error", "not failing") &&
		containsAny(lower, "runbook", "文档", "知识库", "处理手册", "playbook") {
		return IntentKnowledgeQuery, 0.88, "explicit non-incident knowledge request detected"
	}
	if containsAny(lower, "runbook", "文档", "知识库", "怎么处理", "处理手册", "历史故障", "playbook") &&
		containsAny(lower, "metric", "metrics", "log", "logs", "alert", "error rate", "error", "5xx", "500", "失败", "故障", "incident", "告警") {
		return IntentIncidentTriage, 0.86, "knowledge request with incident evidence signals detected"
	}
	if containsAny(lower, "runbook", "文档", "知识库", "怎么处理", "处理手册", "历史故障", "playbook") {
		return IntentKnowledgeQuery, 0.85, "knowledge or runbook signal detected"
	}
	if containsAny(lower, "metric", "metrics", "qps", "error rate", "错误率", "latency", "p95", "指标") {
		if containsAny(lower, "error", "5xx", "500", "失败", "故障", "incident", "告警", "timeout", "超时") {
			return IntentIncidentTriage, 0.86, "metric and incident signals detected"
		}
		return IntentMetricsQuery, 0.78, "metric signal detected"
	}
	if containsAny(lower, "log", "日志", "panic", "exception", "stack") {
		return IntentLogsQuery, 0.8, "log signal detected"
	}
	if containsAny(lower, "error", "5xx", "500", "fail", "failing", "失败", "报错", "异常", "incident", "故障", "告警", "timeout", "超时", "slow", "慢", "排查") {
		return IntentIncidentTriage, 0.82, "incident symptom detected"
	}
	return IntentGeneralChat, 0.5, "no strong diagnostic signal detected"
}

func suggestToolsForIntent(intentType IntentType, traceID string) []ToolName {
	return defaultTools(intentType, nil, traceID)
}

func suggestAgentsForIntent(intentType IntentType) []AgentRole {
	return defaultAgents(intentType, nil)
}

func buildRAGHints(intentType IntentType, message string) RAGHints {
	hints := RAGHints{QueryBoosts: keywordCandidates(message)}
	switch intentType {
	case IntentIncidentTriage:
		hints.PreferRunbooks = true
		hints.PreferIncidents = true
		hints.Categories = []string{"runbook", "incident", "playbook"}
		hints.TopKOverride = 8
	case IntentKnowledgeQuery, IntentMitigation:
		hints.PreferRunbooks = true
		hints.Categories = []string{"runbook", "playbook"}
		hints.TopKOverride = 8
	case IntentTraceAnalysis:
		hints.PreferObservabilityDocs = true
		hints.Categories = []string{"observability", "trace", "runbook"}
	case IntentMetricsQuery, IntentLogsQuery:
		hints.PreferObservabilityDocs = true
		hints.Categories = []string{"observability", "runbook"}
		hints.TopKOverride = 3
	}
	return hints
}

func keywordCandidates(message string) []string {
	parts := strings.FieldsFunc(message, func(r rune) bool {
		return r == ' ' || r == ',' || r == ';' || r == '，' || r == '。' || r == '?' || r == '？'
	})
	result := make([]string, 0, min(len(parts), 8)+1)
	if strings.TrimSpace(message) != "" {
		result = append(result, strings.TrimSpace(message))
	}
	for _, part := range parts {
		part = strings.Trim(part, ".,;:()[]{}")
		if len([]rune(part)) < 3 {
			continue
		}
		result = append(result, part)
		if len(result) >= 8 {
			break
		}
	}
	return dedupeStrings(result)
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}
