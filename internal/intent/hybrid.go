package intent

import (
	"context"
	"errors"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
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
		"intent.recognize",
		attribute.String("intent.mode", h.config.Mode),
	)
	defer span.End()

	if !h.config.Enabled {
		result := SafeDefault(input.Message, IntentLimitation{
			Code:    "INTENT_DISABLED",
			Message: "Intent recognition is disabled; safe default intent was used.",
		})
		h.finish(span, result, true)
		return result, nil
	}

	mode := strings.ToLower(strings.TrimSpace(h.config.Mode))
	if mode == "llm" || mode == "hybrid" {
		if h.config.LLMEnabled && h.llm != nil {
			result, err := h.llm.Recognize(ctx, input)
			if err == nil && result.Confidence >= h.config.MinLLMConfidence {
				result.Source = "llm"
				result.Metadata = ensureMetadata(result.Metadata)
				result.Metadata["fallback_used"] = false
				normalized := Normalize(result)
				h.finish(span, normalized, false)
				return normalized, nil
			}
			reason := "low confidence"
			if err != nil {
				reason = err.Error()
			}
			return h.fallbackToRule(ctx, span, input, reason)
		}
		if mode == "llm" {
			return h.fallbackToRule(ctx, span, input, "intent LLM is not configured")
		}
	}

	result, err := h.rule.Recognize(ctx, input)
	if err != nil {
		result = SafeDefault(input.Message, IntentLimitation{
			Code:    "INTENT_RULE_FALLBACK",
			Message: "Rule-based intent recognition failed; safe default intent was used.",
		})
		h.finish(span, result, true)
		return result, nil
	}
	result.Source = "rule"
	result.Metadata = ensureMetadata(result.Metadata)
	result.Metadata["fallback_used"] = false
	normalized := Normalize(result)
	h.finish(span, normalized, false)
	return normalized, nil
}

func (h *HybridRecognizer) fallbackToRule(
	ctx context.Context,
	span interface{ SetAttributes(...attribute.KeyValue) },
	input RecognitionInput,
	reason string,
) (IntentResult, error) {
	result, err := h.rule.Recognize(ctx, input)
	if err != nil {
		result = SafeDefault(input.Message, IntentLimitation{
			Code:    "INTENT_LLM_FALLBACK",
			Message: "LLM intent recognition failed and rule fallback failed; safe default intent was used.",
		})
		result.Metadata["llm_failed"] = true
		result.Metadata["llm_failure_reason"] = reason
		h.finish(span, result, true)
		return result, nil
	}
	result.Source = "rule"
	result = AddLimitation(
		result,
		"INTENT_LLM_FALLBACK",
		"LLM intent recognition was unavailable or unreliable; rule-based intent was used.",
	)
	result.Metadata = ensureMetadata(result.Metadata)
	result.Metadata["llm_failed"] = true
	result.Metadata["llm_failure_reason"] = reason
	result.Metadata["fallback_used"] = true
	normalized := Normalize(result)
	h.finish(span, normalized, true)
	return normalized, nil
}

func (h *HybridRecognizer) finish(
	span interface{ SetAttributes(...attribute.KeyValue) },
	result IntentResult,
	fallbackUsed bool,
) {
	span.SetAttributes(
		attribute.String("intent.type", string(result.Intent)),
		attribute.String("intent.source", result.Source),
		attribute.Float64("intent.confidence", result.Confidence),
		attribute.Bool("intent.fallback_used", fallbackUsed),
		attribute.Int("selected_tools_count", len(result.SuggestedTools)),
		attribute.Int("selected_agents_count", len(result.SuggestedAgents)),
	)
}

func ensureMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

type failingRecognizer struct{}

func (failingRecognizer) Recognize(context.Context, RecognitionInput) (IntentResult, error) {
	return IntentResult{}, errors.New("recognizer failed")
}
