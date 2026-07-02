package eino

import (
	"context"

	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"go.opentelemetry.io/otel/attribute"
)

type FallbackRunner struct {
	primary  AgentRunner
	fallback AgentRunner
}

func NewFallbackRunner(primary AgentRunner, fallback AgentRunner) *FallbackRunner {
	return &FallbackRunner{primary: primary, fallback: fallback}
}

func (r *FallbackRunner) Run(ctx context.Context, input AgentInput) (AgentOutput, error) {
	output, err := r.primary.Run(ctx, input)
	if err == nil {
		return output, nil
	}
	return r.runFallback(ctx, input, err)
}

func (r *FallbackRunner) RenderPrompt(
	ctx context.Context,
	input AgentInput,
) ([]*schema.Message, error) {
	renderer, ok := r.primary.(PromptRenderingRunner)
	if !ok {
		return nil, nil
	}
	return renderer.RenderPrompt(ctx, input)
}

func (r *FallbackRunner) RunPrepared(
	ctx context.Context,
	input AgentInput,
	messages []*schema.Message,
) (AgentOutput, error) {
	renderer, ok := r.primary.(PromptRenderingRunner)
	if !ok {
		return r.Run(ctx, input)
	}
	output, err := renderer.RunPrepared(ctx, input, messages)
	if err == nil {
		return output, nil
	}
	return r.runFallback(ctx, input, err)
}

func (r *FallbackRunner) runFallback(
	ctx context.Context,
	input AgentInput,
	primaryErr error,
) (AgentOutput, error) {
	if ctx.Err() != nil {
		return AgentOutput{}, primaryErr
	}
	runtimemetrics.IncAgentFallback("llm_unavailable")

	ctx, span := observability.StartSpan(
		ctx,
		"agent.fallback",
		attribute.Bool("fallback_used", true),
		attribute.String("fallback_runner", "deterministic"),
	)
	defer span.End()
	output, fallbackErr := r.fallback.Run(ctx, input)
	if fallbackErr != nil {
		observability.MarkError(span, "Agent fallback failed")
		return AgentOutput{}, fallbackErr
	}
	if output.Metadata == nil {
		output.Metadata = map[string]any{}
	}
	output.Metadata["fallback_used"] = true
	output.Metadata["fallback_reason"] = "llm_unavailable"
	output.Limitations = append(output.Limitations, Limitation{
		Code:    "AGENT_LLM_FALLBACK",
		Message: "The LLM Agent was unavailable; the deterministic runner handled this request.",
	})
	return output, nil
}

var _ PromptRenderingRunner = (*FallbackRunner)(nil)
