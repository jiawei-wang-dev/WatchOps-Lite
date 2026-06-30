package eino

import (
	"context"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
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
	if ctx.Err() != nil {
		return AgentOutput{}, err
	}

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
