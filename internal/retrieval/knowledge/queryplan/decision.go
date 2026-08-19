package queryplan

import (
	"strings"
	"unicode/utf8"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

type MultiQueryDecision struct {
	UseMultiQuery bool   `json:"use_multi_query"`
	Reason        string `json:"reason"`
}

// DecideMultiQuery is deterministic. Missing slots are intentionally not a
// trigger: slot validation and clarification own that decision.
func DecideMultiQuery(input QueryPlanInput) MultiQueryDecision {
	message := strings.ToLower(strings.TrimSpace(input.UserMessage))
	if isCorrectionRequest(message) {
		return MultiQueryDecision{Reason: "authoritative_parameter_correction"}
	}
	if strings.TrimSpace(input.Intent.TraceID) != "" {
		return MultiQueryDecision{Reason: "explicit_trace_id"}
	}
	switch input.Intent.Intent {
	case intent.IntentMetricsQuery:
		return MultiQueryDecision{Reason: "explicit_metrics_query"}
	case intent.IntentLogsQuery:
		return MultiQueryDecision{Reason: "explicit_logs_query"}
	case intent.IntentTraceAnalysis:
		return MultiQueryDecision{Reason: "explicit_trace_query"}
	case intent.IntentKnowledgeQuery:
		return MultiQueryDecision{Reason: "simple_knowledge_query"}
	case intent.IntentIncidentTriage:
		if reason := incidentComplexityReason(message); reason != "" {
			return MultiQueryDecision{UseMultiQuery: true, Reason: reason}
		}
		return MultiQueryDecision{Reason: "simple_incident_query"}
	default:
		return MultiQueryDecision{Reason: "simple_or_non_diagnostic_query"}
	}
}

func ShouldUseMultiQuery(input QueryPlanInput) bool {
	return DecideMultiQuery(input).UseMultiQuery
}

// AuthoritativeQuery removes obsolete service names from correction turns.
// Structured current-turn service wins over anything still present in text.
func AuthoritativeQuery(input QueryPlanInput) string {
	original := strings.TrimSpace(input.UserMessage)
	if !isCorrectionRequest(strings.ToLower(original)) {
		if input.Intent.Intent == intent.IntentTraceAnalysis && strings.TrimSpace(input.Intent.TraceID) == "" {
			service := firstNonEmpty(input.Service, input.Intent.Service, serviceFromText(original))
			return strings.TrimSpace(strings.Join([]string{service, original, "trace window anomaly investigation guide"}, " "))
		}
		return original
	}
	service := firstNonEmpty(input.Service, input.Intent.Service, serviceFromText(original))
	intentTerm := ""
	switch input.Intent.Intent {
	case intent.IntentMetricsQuery:
		intentTerm = "metrics"
	case intent.IntentLogsQuery:
		intentTerm = "logs"
	case intent.IntentTraceAnalysis:
		intentTerm = "trace"
	case intent.IntentKnowledgeQuery:
		intentTerm = "knowledge"
	}
	return strings.TrimSpace(strings.Join([]string{
		service, intentTerm, "service switch parameter correction handling runbook guide",
	}, " "))
}

func isCorrectionRequest(message string) bool {
	return containsAny(message,
		"不是", "改成", "换成", "改查", "切到", "别查", "不用查",
		"instead", "switch to", "change to", "not ", "rather than",
	)
}

func incidentComplexityReason(message string) string {
	switch {
	case containsAny(message, "dependency", "depends on", "依赖", "导致", "拖慢", "因果", "because", "due to"):
		return "incident_dependency_or_causal_chain"
	case containsAny(message, "error chain", "timeout chain", "failure chain", "级联", "链式故障"):
		return "incident_error_chain"
	case symptomSignalCount(message) > 1:
		return "incident_multiple_symptoms"
	case utf8.RuneCountInString(message) >= 48 && containsAny(message, "结合", "同时", "并且", " and "):
		return "incident_complex_description"
	default:
		return ""
	}
}

func symptomSignalCount(message string) int {
	count := 0
	for _, matched := range []bool{
		containsAny(message, "error", "错误", "失败", "5xx"),
		containsAny(message, "latency", "延迟", "slow", "慢", "p95"),
		containsAny(message, "timeout", "deadline", "超时"),
	} {
		if matched {
			count++
		}
	}
	return count
}
