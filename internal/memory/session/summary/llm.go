package summary

import (
	"context"
	"errors"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type ChatModel interface {
	Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error)
}

type LLMConfig struct {
	PromptVersion string
	Timeout       time.Duration
}

type LLM struct {
	model    ChatModel
	fallback session.Summarizer
	config   LLMConfig
}

func NewLLM(
	chatModel ChatModel,
	fallback session.Summarizer,
	config LLMConfig,
) (*LLM, error) {
	if chatModel == nil {
		return nil, errors.New("summary ChatModel is required")
	}
	if fallback == nil {
		return nil, errors.New("deterministic summary fallback is required")
	}
	if config.PromptVersion == "" {
		return nil, errors.New("summary prompt version is required")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("summary timeout must be greater than zero")
	}
	return &LLM{model: chatModel, fallback: fallback, config: config}, nil
}

func (s *LLM) Summarize(
	ctx context.Context,
	current session.Summary,
	messages []session.Message,
) (result session.Summary, resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"summary.llm",
		attribute.String("summary.mode", "llm"),
		attribute.String("summary.prompt_version", s.config.PromptVersion),
		attribute.Int("message_count", len(messages)),
	)
	fallbackUsed := false
	parseSuccess := false
	defer func() {
		span.SetAttributes(
			attribute.Bool("fallback_used", fallbackUsed),
			attribute.Bool("parse_success", parseSuccess),
		)
		if resultErr != nil {
			observability.MarkError(span, "session summary failed")
		}
		span.End()
	}()

	prompt, err := renderPrompt(s.config.PromptVersion, current, messages)
	if err != nil {
		fallbackUsed = true
		return s.runFallback(ctx, current, messages, "prompt_render")
	}

	callContext, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	response, err := s.model.Generate(callContext, prompt)
	if err != nil || response == nil {
		fallbackUsed = true
		return s.runFallback(ctx, current, messages, "model_call")
	}

	parseContext, parseSpan := observability.StartSpan(
		ctx,
		"summary.parse",
		attribute.String("summary.prompt_version", s.config.PromptVersion),
	)
	result, err = parseSummary(response.Content, current)
	parseSuccess = err == nil
	parseSpan.SetAttributes(attribute.Bool("parse_success", parseSuccess))
	if err != nil {
		observability.MarkError(parseSpan, "summary parse failed")
	}
	parseSpan.End()
	if err != nil {
		fallbackUsed = true
		return s.runFallback(parseContext, current, messages, "parse")
	}
	return result, nil
}

func (s *LLM) runFallback(
	ctx context.Context,
	current session.Summary,
	messages []session.Message,
	reason string,
) (session.Summary, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"summary.fallback",
		attribute.String("summary.mode", "deterministic"),
		attribute.String("summary.prompt_version", s.config.PromptVersion),
		attribute.String("fallback_reason", reason),
		attribute.Bool("fallback_used", true),
		attribute.Int("message_count", len(messages)),
	)
	defer span.End()
	result, err := s.fallback.Summarize(ctx, current, messages)
	if err != nil {
		observability.MarkError(span, "deterministic summary fallback failed")
	}
	return result, err
}

var _ session.Summarizer = (*LLM)(nil)
