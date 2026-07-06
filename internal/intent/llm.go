package intent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type ChatModel interface {
	Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error)
}

type LLMRecognizer struct {
	model   ChatModel
	timeout time.Duration
}

func NewLLMRecognizer(chatModel ChatModel, timeout time.Duration) (*LLMRecognizer, error) {
	if chatModel == nil {
		return nil, errors.New("intent ChatModel is required")
	}
	if timeout <= 0 {
		timeout = DefaultConfig().Timeout
	}
	return &LLMRecognizer{model: chatModel, timeout: timeout}, nil
}

func (r *LLMRecognizer) Recognize(
	ctx context.Context,
	input RecognitionInput,
) (IntentResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"intent.llm",
		attribute.Int("message_length", len(input.Message)),
	)
	defer span.End()

	callContext, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	response, err := r.model.Generate(callContext, []*schema.Message{
		schema.SystemMessage(intentSystemPrompt),
		schema.UserMessage(buildIntentPrompt(input)),
	})
	if err != nil {
		observability.MarkError(span, "intent LLM call failed")
		return IntentResult{}, err
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		observability.MarkError(span, "intent LLM returned empty response")
		return IntentResult{}, errors.New("intent LLM returned empty response")
	}
	result, err := parseIntentJSON(response.Content)
	if err != nil {
		observability.MarkError(span, "intent LLM JSON parse failed")
		return IntentResult{}, err
	}
	result.Source = "llm"
	normalized := Normalize(result)
	span.SetAttributes(
		attribute.String("intent.type", string(normalized.Intent)),
		attribute.Float64("intent.confidence", normalized.Confidence),
	)
	return normalized, nil
}

func parseIntentJSON(value string) (IntentResult, error) {
	candidate := stripJSONFence(value)
	var result IntentResult
	if err := json.Unmarshal([]byte(candidate), &result); err == nil {
		return result, nil
	}
	start := strings.Index(candidate, "{")
	end := strings.LastIndex(candidate, "}")
	if start < 0 || end <= start {
		return IntentResult{}, errors.New("intent response does not contain a JSON object")
	}
	if err := json.Unmarshal([]byte(candidate[start:end+1]), &result); err != nil {
		return IntentResult{}, err
	}
	return result, nil
}

func stripJSONFence(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		value = strings.TrimPrefix(value, "```json")
		value = strings.TrimPrefix(value, "```")
		value = strings.TrimSuffix(value, "```")
	}
	return strings.TrimSpace(value)
}
