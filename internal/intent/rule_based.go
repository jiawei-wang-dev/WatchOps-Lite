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
	traceIDPattern   = regexp.MustCompile(`(?i)\b[0-9a-f]{16}(?:[0-9a-f]{16})?\b`)
	servicePattern   = regexp.MustCompile(`(?i)\b[a-z][a-z0-9_.-]*(?:-service|_service|\.service|service|gateway)\b`)
	timeRangePattern = regexp.MustCompile(`(?i)(?:last|最近|过去)\s*(\d{1,3})\s*(?:m|min|minute|minutes|分钟)`)
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
	result := IntentResult{
		Intent:          intentType,
		Confidence:      confidence,
		Reason:          reason,
		Service:         detectService(message),
		TimeRange:       detectTimeRange(message, input.Now),
		TraceID:         traceID,
		Symptom:         detectSymptom(message),
		Keywords:        keywordCandidates(message),
		SuggestedTools:  suggestToolsForIntent(intentType, traceID),
		SuggestedAgents: suggestAgentsForIntent(intentType),
		RAGHints:        buildRAGHints(intentType, message),
		Source:          "rule",
		Metadata:        map[string]any{"rule_based": true},
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
		return ""
	}
	return strings.Trim(match, ".,;:()[]{}")
}

func detectTimeRange(message string, now time.Time) *TimeRangeHint {
	match := timeRangePattern.FindStringSubmatch(message)
	if len(match) < 2 {
		return nil
	}
	return &TimeRangeHint{Relative: "last_" + match[1] + "_minutes"}
}

func detectSymptom(message string) string {
	lower := strings.ToLower(message)
	switch {
	case containsAny(lower, "timeout", "超时", "deadline"):
		return "timeout"
	case containsAny(lower, "latency", "slow", "p95", "耗时", "慢"):
		return "latency"
	case containsAny(lower, "error", "5xx", "500", "失败", "报错", "异常"):
		return "error"
	case containsAny(lower, "panic", "exception", "stack"):
		return "exception"
	default:
		return ""
	}
}

func classifyIntentByKeywords(message string, traceID string) (IntentType, float64, string) {
	lower := strings.ToLower(message)
	if traceID != "" || containsAny(lower, "trace", "span", "链路", "慢调用") {
		return IntentTraceAnalysis, 0.9, "trace signal detected"
	}
	if containsAny(lower, "runbook", "文档", "知识库", "怎么处理", "处理手册", "历史故障", "playbook") {
		return IntentKnowledgeQuery, 0.85, "knowledge or runbook signal detected"
	}
	if containsAny(lower, "metric", "metrics", "qps", "error rate", "latency", "p95", "指标") {
		if containsAny(lower, "error", "5xx", "500", "失败", "故障", "incident", "告警", "timeout", "超时") {
			return IntentIncidentTriage, 0.86, "metric and incident signals detected"
		}
		return IntentMetricsQuery, 0.78, "metric signal detected"
	}
	if containsAny(lower, "log", "日志", "panic", "exception", "stack") {
		return IntentLogsQuery, 0.8, "log signal detected"
	}
	if containsAny(lower, "error", "5xx", "500", "fail", "failing", "失败", "报错", "异常", "incident", "故障", "告警", "timeout", "超时", "slow", "慢") {
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
