package intent

import (
	"sort"
	"strings"
	"time"
)

type DecisionType string

const (
	DecisionProceed  DecisionType = "proceed"
	DecisionClarify  DecisionType = "clarify"
	DecisionFallback DecisionType = "fallback"
)

type IntentDecision struct {
	Result          IntentResult      `json:"result"`
	KnownSlots      map[string]string `json:"known_slots"`
	MissingRequired []string          `json:"missing_required"`
	MissingOptional []string          `json:"missing_optional"`
	Ambiguous       bool              `json:"ambiguous"`
	Decision        DecisionType      `json:"decision"`
	ReasonCode      string            `json:"reason_code"`
	ClarifyQuestion string            `json:"clarify_question,omitempty"`
	DefaultsApplied map[string]string `json:"defaults_applied"`
}

type SlotRule struct {
	Required    []string          `json:"required"`
	RequiredAny [][]string        `json:"required_any"`
	Optional    []string          `json:"optional"`
	Defaults    map[string]string `json:"defaults"`
}

var slotRules = map[IntentType]SlotRule{
	IntentMetricsQuery:   {Required: []string{"service"}, Optional: []string{"time_range", "metric_type"}},
	IntentLogsQuery:      {Required: []string{"service"}, Optional: []string{"time_range", "level", "keywords"}},
	IntentIncidentTriage: {Required: []string{"service"}, Optional: []string{"time_range", "symptom", "environment"}},
	IntentTraceAnalysis:  {RequiredAny: [][]string{{"trace_id"}, {"service", "time_range"}}},
	IntentKnowledgeQuery: {},
	IntentMitigation:     {},
	IntentStatusSummary:  {RequiredAny: [][]string{{"service"}, {"trace_id"}, {"task"}}},
	IntentGeneralChat:    {},
}

func SlotRules() map[IntentType]SlotRule {
	result := make(map[IntentType]SlotRule, len(slotRules))
	for key, rule := range slotRules {
		result[key] = rule
	}
	return result
}

// ValidateSlots applies deterministic precedence: current recognized values,
// structured command slots, confirmed focus slots, then explicit safe defaults.
func ValidateSlots(
	message string,
	result IntentResult,
	commandSlots map[string]string,
	focus FocusView,
	minConfidence float64,
) IntentDecision {
	result = Normalize(result)
	if minConfidence <= 0 {
		minConfidence = 0.55
	}
	known := focusSlots(focus)
	mergeNonEmpty(known, resultSlots(result))
	mergeNonEmpty(known, commandSlots)
	// Re-apply slots that are syntactically explicit in this turn. This keeps
	// the documented precedence even when a context-aware recognizer copied
	// older Focus values into its normalized result.
	explicit := map[string]string{
		"service":  detectService(message),
		"trace_id": detectTraceID(message),
		"symptom":  detectSymptom(message),
	}
	if value := detectTimeRange(message, time.Time{}); value != nil {
		explicit["time_range"] = resultSlots(IntentResult{TimeRange: value})["time_range"]
	}
	mergeNonEmpty(known, explicit)
	applyKnownSlots(&result, known)
	decision := IntentDecision{
		Result:          result,
		KnownSlots:      known,
		MissingRequired: []string{},
		MissingOptional: []string{},
		DefaultsApplied: map[string]string{},
		Decision:        DecisionProceed,
		ReasonCode:      "SLOTS_COMPLETE",
	}

	lower := strings.ToLower(strings.TrimSpace(message))
	if result.Intent != IntentGeneralChat && len(commonServicePattern.FindAllString(message, -1)) > 1 {
		return clarifyDecision(decision, "AMBIGUOUS_SERVICE",
			"你想先排查哪个服务？例如 checkout 或 payment。")
	}
	if ordinal := referencedCandidateIndex(message); ordinal >= 0 &&
		len(focus.Candidates) <= ordinal {
		return clarifyDecision(decision, "AMBIGUOUS_REFERENCE",
			"你指的是哪个选项？请补充具体服务或对象。")
	}
	if isContextReference(message) && !focus.Available {
		return clarifyDecision(decision, "AMBIGUOUS_REFERENCE",
			"当前会话没有可恢复的上一轮对象，请补充具体服务或排障目标。")
	}
	if result.Intent == IntentGeneralChat && detectService(message) != "" &&
		containsAny(lower, "看看", "看一下", "查", "check ", "look at") {
		return clarifyDecision(decision, "AMBIGUOUS_INTENT",
			"你想查看 "+known["service"]+" 的指标、日志，还是分析当前故障？")
	}
	if result.Intent != IntentGeneralChat && result.Confidence < minConfidence {
		return clarifyDecision(decision, "LOW_CONFIDENCE",
			"我还不能确定你的排障目标，请说明要查指标、日志、Trace，还是当前故障。")
	}

	rule := slotRules[result.Intent]
	for _, slot := range rule.Required {
		if strings.TrimSpace(known[slot]) == "" {
			decision.MissingRequired = append(decision.MissingRequired, slot)
		}
	}
	if len(rule.RequiredAny) > 0 {
		satisfied := false
		for _, group := range rule.RequiredAny {
			groupComplete := true
			for _, slot := range group {
				if strings.TrimSpace(known[slot]) == "" {
					groupComplete = false
					break
				}
			}
			if groupComplete {
				satisfied = true
				break
			}
		}
		if !satisfied {
			missing := rule.RequiredAny[0]
			if result.Intent == IntentTraceAnalysis {
				missing = []string{"trace_id", "service", "time_range"}
			}
			decision.MissingRequired = append(decision.MissingRequired, missing...)
		}
	}
	for _, slot := range rule.Optional {
		if strings.TrimSpace(known[slot]) == "" {
			decision.MissingOptional = append(decision.MissingOptional, slot)
		}
	}
	decision.MissingRequired = uniqueSorted(decision.MissingRequired)
	if len(decision.MissingRequired) > 0 {
		question := questionFor(result.Intent, decision.MissingRequired)
		return clarifyDecision(decision, "MISSING_REQUIRED_SLOT", question)
	}
	return decision
}

func clarifyDecision(value IntentDecision, reason, question string) IntentDecision {
	value.Decision = DecisionClarify
	value.Ambiguous = strings.HasPrefix(reason, "AMBIGUOUS") || reason == "LOW_CONFIDENCE"
	value.ReasonCode = reason
	value.ClarifyQuestion = question
	return value
}

func questionFor(intentType IntentType, missing []string) string {
	if intentType == IntentStatusSummary {
		return "你想总结当前哪个服务或哪一次排障？"
	}
	if containsString(missing, "service") && intentType != IntentTraceAnalysis {
		return "需要排查哪个服务？例如 checkout 或 payment。"
	}
	if intentType == IntentTraceAnalysis {
		return "请提供 Trace ID；或者补充服务名和时间范围。"
	}
	return "请补充继续处理所需的关键信息。"
}

func focusSlots(focus FocusView) map[string]string {
	result := map[string]string{}
	mergeNonEmpty(result, focus.KnownSlots)
	return result
}

func resultSlots(result IntentResult) map[string]string {
	slots := map[string]string{
		"service":  result.Service,
		"symptom":  result.Symptom,
		"trace_id": result.TraceID,
	}
	if result.TimeRange != nil {
		if result.TimeRange.Relative != "" {
			slots["time_range"] = result.TimeRange.Relative
		} else if result.TimeRange.From != "" || result.TimeRange.To != "" {
			slots["time_range"] = result.TimeRange.From + "/" + result.TimeRange.To
		}
	}
	return slots
}

func applyKnownSlots(result *IntentResult, known map[string]string) {
	result.Service = known["service"]
	result.Symptom = known["symptom"]
	result.TraceID = known["trace_id"]
	if value := known["time_range"]; value != "" {
		result.TimeRange = &TimeRangeHint{Relative: value}
	}
}

func mergeNonEmpty(target, source map[string]string) {
	for key, value := range source {
		if value = strings.TrimSpace(value); value != "" {
			target[key] = value
		}
	}
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
