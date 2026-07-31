package intent

import (
	"context"
	"errors"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const (
	escalationNone              = ""
	escalationRuleError         = "RULE_ERROR"
	escalationRuleIntentEmpty   = "RULE_INTENT_EMPTY"
	escalationRuleSafeDefault   = "RULE_SAFE_DEFAULT"
	escalationRuleLowConfidence = "RULE_LOW_CONFIDENCE"
	escalationSignalConflict    = "RULE_SIGNAL_CONFLICT"
	escalationRuleAmbiguous     = "RULE_AMBIGUOUS"
	escalationContextUnresolved = "RULE_CONTEXT_UNRESOLVED"
)

type HybridRecognizer struct {
	config Config
	llm    Recognizer
	rule   Recognizer
}

func NewHybridRecognizer(config Config, llm Recognizer, rule Recognizer) *HybridRecognizer {
	config = config.Normalize()
	if rule == nil {
		rule = NewRuleBasedRecognizer()
	}
	return &HybridRecognizer{config: config, llm: llm, rule: rule}
}

func (h *HybridRecognizer) Recognize(
	ctx context.Context,
	input RecognitionInput,
) (IntentResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"intent.hybrid",
		attribute.String("intent.mode", h.config.Mode),
	)
	defer span.End()

	if !h.config.Enabled {
		result := SafeDefault(input.Message, IntentLimitation{
			Code:    "INTENT_DISABLED",
			Message: "Intent recognition is disabled; safe default intent was used.",
		})
		result = h.finalize(result, 0, false, 0, escalationNone, true)
		h.finish(span, result)
		return result, nil
	}

	var result IntentResult
	switch strings.ToLower(strings.TrimSpace(h.config.Mode)) {
	case "rule":
		result = h.recognizeRuleOnly(ctx, input)
	case "llm":
		result = h.recognizeLLMFirst(ctx, input)
	default:
		result = h.recognizeRuleFirst(ctx, input)
	}
	h.finish(span, result)
	return result, nil
}

func (h *HybridRecognizer) recognizeRuleOnly(
	ctx context.Context,
	input RecognitionInput,
) IntentResult {
	ruleResult, ruleErr := h.rule.Recognize(ctx, input)
	if ruleErr != nil {
		return h.finalize(
			SafeDefault(input.Message, IntentLimitation{
				Code:    "INTENT_RULE_FALLBACK",
				Message: "Rule-based intent recognition failed; safe default intent was used.",
			}),
			0,
			false,
			0,
			escalationRuleError,
			true,
		)
	}
	ruleResult = withSource(ruleResult, "rule")
	return h.finalize(
		ruleResult,
		ruleResult.Confidence,
		false,
		0,
		escalationNone,
		isSafeDefault(ruleResult),
	)
}

func (h *HybridRecognizer) recognizeLLMFirst(
	ctx context.Context,
	input RecognitionInput,
) IntentResult {
	if h.config.LLMEnabled && h.llm != nil {
		llmResult, llmErr := h.llm.Recognize(ctx, input)
		llmConfidence := normalizedConfidence(llmResult, llmErr)
		if llmErr == nil && llmResult.Intent != "" &&
			llmConfidence >= h.config.MinLLMConfidence {
			llmResult = withSource(llmResult, "llm")
			return h.finalize(
				llmResult,
				0,
				true,
				llmConfidence,
				"MODE_LLM",
				false,
			)
		}
		return h.llmFallbackToRule(
			ctx,
			input,
			true,
			llmConfidence,
			llmFallbackReason(llmErr, llmResult, h.config.MinLLMConfidence),
		)
	}
	return h.llmFallbackToRule(
		ctx,
		input,
		false,
		0,
		"LLM_UNAVAILABLE",
	)
}

func (h *HybridRecognizer) recognizeRuleFirst(
	ctx context.Context,
	input RecognitionInput,
) IntentResult {
	ruleResult, ruleErr := h.rule.Recognize(ctx, input)
	if ruleErr == nil {
		ruleResult = withSource(ruleResult, "rule")
	}
	escalate, reason := h.shouldEscalateToLLM(ruleResult, ruleErr)
	ruleConfidence := normalizedConfidence(ruleResult, ruleErr)
	if !escalate || !h.config.LLMEnabled {
		if ruleErr != nil {
			return h.finalize(
				SafeDefault(input.Message, IntentLimitation{
					Code:    "INTENT_RULE_FALLBACK",
					Message: "Rule-based intent recognition failed; safe default intent was used.",
				}),
				ruleConfidence,
				false,
				0,
				reason,
				true,
			)
		}
		return h.finalize(
			ruleResult,
			ruleConfidence,
			false,
			0,
			reason,
			isSafeDefault(ruleResult),
		)
	}
	if h.llm == nil {
		return h.ruleAfterLLMFallback(
			input,
			ruleResult,
			ruleErr,
			ruleConfidence,
			false,
			0,
			reason,
		)
	}

	llmResult, llmErr := h.llm.Recognize(ctx, input)
	llmConfidence := normalizedConfidence(llmResult, llmErr)
	if llmErr == nil && llmResult.Intent != "" &&
		llmConfidence >= h.config.MinLLMConfidence {
		llmResult = mergeLLMWithRuleAndExplicitSlots(input, llmResult, ruleResult)
		llmResult = withSource(llmResult, "llm")
		return h.finalize(
			llmResult,
			ruleConfidence,
			true,
			llmConfidence,
			reason,
			false,
		)
	}
	return h.ruleAfterLLMFallback(
		input,
		ruleResult,
		ruleErr,
		ruleConfidence,
		true,
		llmConfidence,
		reason,
	)
}

func (h *HybridRecognizer) llmFallbackToRule(
	ctx context.Context,
	input RecognitionInput,
	llmAttempted bool,
	llmConfidence float64,
	reason string,
) IntentResult {
	ruleResult, ruleErr := h.rule.Recognize(ctx, input)
	ruleConfidence := normalizedConfidence(ruleResult, ruleErr)
	return h.ruleAfterLLMFallback(
		input,
		ruleResult,
		ruleErr,
		ruleConfidence,
		llmAttempted,
		llmConfidence,
		reason,
	)
}

func (h *HybridRecognizer) ruleAfterLLMFallback(
	input RecognitionInput,
	ruleResult IntentResult,
	ruleErr error,
	ruleConfidence float64,
	llmAttempted bool,
	llmConfidence float64,
	reason string,
) IntentResult {
	if ruleErr != nil || ruleResult.Intent == "" || isSafeDefault(ruleResult) {
		result := SafeDefault(input.Message, IntentLimitation{
			Code:    "INTENT_LLM_FALLBACK",
			Message: "Intent recognizers were unavailable or unreliable; safe default intent was used.",
		})
		return h.finalize(
			result,
			ruleConfidence,
			llmAttempted,
			llmConfidence,
			reason,
			true,
		)
	}
	ruleResult = withSource(ruleResult, "rule")
	ruleResult = AddLimitation(
		ruleResult,
		"INTENT_LLM_FALLBACK",
		"LLM intent recognition was unavailable or unreliable; rule-based intent was used.",
	)
	return h.finalize(
		ruleResult,
		ruleConfidence,
		llmAttempted,
		llmConfidence,
		reason,
		true,
	)
}

// shouldEscalateToLLM is the single policy boundary for hybrid escalation.
// It relies on structured recognizer metadata, never error text or prompts.
func (h *HybridRecognizer) shouldEscalateToLLM(
	ruleResult IntentResult,
	ruleErr error,
) (bool, string) {
	if ruleErr != nil {
		return true, escalationRuleError
	}
	if ruleResult.Intent == "" {
		return true, escalationRuleIntentEmpty
	}
	if isSafeDefault(ruleResult) {
		return true, escalationRuleSafeDefault
	}
	if metadataBoolValue(ruleResult.Metadata, "intent_signal_conflict") {
		return true, escalationSignalConflict
	}
	if metadataBoolValue(ruleResult.Metadata, "ambiguous") {
		return true, escalationRuleAmbiguous
	}
	if metadataBoolValue(ruleResult.Metadata, "context_reference_unresolved") {
		return true, escalationContextUnresolved
	}
	if ruleResult.Confidence < h.config.MinRuleConfidence {
		return true, escalationRuleLowConfidence
	}
	return false, escalationNone
}

func mergeLLMWithRuleAndExplicitSlots(
	input RecognitionInput,
	llmResult IntentResult,
	ruleResult IntentResult,
) IntentResult {
	llmResult = Normalize(llmResult)
	ruleResult = Normalize(ruleResult)
	if llmResult.Service == "" {
		llmResult.Service = ruleResult.Service
	}
	if llmResult.TraceID == "" {
		llmResult.TraceID = ruleResult.TraceID
	}
	if llmResult.TimeRange == nil {
		llmResult.TimeRange = ruleResult.TimeRange
	}
	if llmResult.Symptom == "" {
		llmResult.Symptom = ruleResult.Symptom
	}

	// Explicit current-message values are deterministic facts and cannot be
	// replaced by an LLM. Focus/HTTP precedence remains in Turn Governance.
	if service := detectService(input.Message); service != "" {
		llmResult.Service = service
	}
	if traceID := detectTraceID(input.Message); traceID != "" {
		llmResult.TraceID = traceID
	}
	if value := detectTimeRange(input.Message, input.Now); value != nil {
		llmResult.TimeRange = value
	}
	if symptom := detectSymptom(input.Message); symptom != "" {
		llmResult.Symptom = symptom
	}
	return llmResult
}

func (h *HybridRecognizer) finalize(
	result IntentResult,
	ruleConfidence float64,
	llmAttempted bool,
	llmConfidence float64,
	escalationReason string,
	fallbackUsed bool,
) IntentResult {
	result = Normalize(result)
	result.Metadata = ensureMetadata(result.Metadata)
	result.Metadata["rule_confidence"] = ruleConfidence
	result.Metadata["llm_attempted"] = llmAttempted
	result.Metadata["llm_confidence"] = llmConfidence
	result.Metadata["escalation_reason"] = escalationReason
	result.Metadata["fallback_used"] = fallbackUsed
	result.Metadata["final_source"] = result.Source
	return result
}

func (h *HybridRecognizer) finish(
	span interface{ SetAttributes(...attribute.KeyValue) },
	result IntentResult,
) {
	span.SetAttributes(
		attribute.String("intent.type", string(result.Intent)),
		attribute.String("intent.source", result.Source),
		attribute.Float64("intent.confidence", result.Confidence),
		attribute.Float64("intent.rule_confidence", metadataFloat64(result.Metadata, "rule_confidence")),
		attribute.Bool("intent.llm_attempted", metadataBoolValue(result.Metadata, "llm_attempted")),
		attribute.Float64("intent.llm_confidence", metadataFloat64(result.Metadata, "llm_confidence")),
		attribute.String("intent.escalation_reason", metadataStringValue(result.Metadata, "escalation_reason")),
		attribute.Bool("intent.fallback_used", metadataBoolValue(result.Metadata, "fallback_used")),
		attribute.Int("selected_tools_count", len(result.SuggestedTools)),
		attribute.Int("selected_agents_count", len(result.SuggestedAgents)),
	)
}

func withSource(result IntentResult, source string) IntentResult {
	result.Source = source
	result.Metadata = ensureMetadata(result.Metadata)
	return Normalize(result)
}

func normalizedConfidence(result IntentResult, err error) float64 {
	if err != nil {
		return 0
	}
	return Normalize(result).Confidence
}

func isSafeDefault(result IntentResult) bool {
	return strings.EqualFold(strings.TrimSpace(result.Source), "fallback") ||
		metadataBoolValue(result.Metadata, "fallback_used")
}

func llmFallbackReason(err error, result IntentResult, threshold float64) string {
	switch {
	case err != nil:
		return "LLM_ERROR"
	case result.Intent == "":
		return "LLM_INTENT_EMPTY"
	case result.Confidence < threshold:
		return "LLM_LOW_CONFIDENCE"
	default:
		return "LLM_UNAVAILABLE"
	}
}

func ensureMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

func metadataBoolValue(metadata map[string]any, key string) bool {
	value, _ := metadata[key].(bool)
	return value
}

func metadataStringValue(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return value
}

func metadataFloat64(metadata map[string]any, key string) float64 {
	switch value := metadata[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	default:
		return 0
	}
}

type failingRecognizer struct{}

func (failingRecognizer) Recognize(context.Context, RecognitionInput) (IntentResult, error) {
	return IntentResult{}, errors.New("recognizer failed")
}
