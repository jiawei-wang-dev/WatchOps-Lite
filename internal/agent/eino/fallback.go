package eino

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/control"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"go.opentelemetry.io/otel/attribute"
)

type FallbackRunner struct {
	primary  AgentRunner
	fallback AgentRunner
	control  *control.Controller
}

func NewFallbackRunner(primary AgentRunner, fallback AgentRunner) *FallbackRunner {
	return NewFallbackRunnerWithControl(primary, fallback, control.DefaultConfig())
}

func NewFallbackRunnerWithControl(
	primary AgentRunner,
	fallback AgentRunner,
	config control.Config,
) *FallbackRunner {
	if control.IsZero(config) {
		config = control.DefaultConfig()
	}
	return &FallbackRunner{
		primary:  primary,
		fallback: fallback,
		control:  control.New(config),
	}
}

func (r *FallbackRunner) Run(ctx context.Context, input AgentInput) (AgentOutput, error) {
	output, err := r.primary.Run(ctx, input)
	if err == nil {
		return r.maybeControlledFallback(ctx, input, output)
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
		return r.maybeControlledFallback(ctx, input, output)
	}
	return r.runFallback(ctx, input, err)
}

func (r *FallbackRunner) maybeControlledFallback(
	ctx context.Context,
	input AgentInput,
	output AgentOutput,
) (AgentOutput, error) {
	if r.control == nil {
		return output, nil
	}
	shouldFallback, reason := outputNeedsControlledFallback(output)
	if !shouldFallback {
		return output, nil
	}
	r.control.MarkFallback(ctx, reason)
	return r.runFallback(ctx, input, controlledFallbackError{reason: reason})
}

func (r *FallbackRunner) runFallback(
	ctx context.Context,
	input AgentInput,
	primaryErr error,
) (AgentOutput, error) {
	if ctx.Err() != nil {
		return AgentOutput{}, primaryErr
	}
	reason := fallbackReason(primaryErr)
	errorType, safeMessage := fallbackErrorSummary(primaryErr)
	runtimemetrics.IncAgentFallback(reason)

	ctx, span := observability.StartSpan(
		ctx,
		"agent.fallback",
		attribute.Bool("fallback_used", true),
		attribute.String("fallback_runner", "deterministic"),
		attribute.String("fallback_reason", reason),
		attribute.String("primary_error_type", errorType),
		attribute.String("primary_error_safe_message", safeMessage),
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
	output.Metadata["fallback_reason"] = reason
	output.Metadata["primary_error_type"] = errorType
	output.Metadata["primary_error_safe_message"] = safeMessage
	output.Limitations = append(output.Limitations, Limitation{
		Code: "AGENT_LLM_FALLBACK",
		Message: localizedText(
			prefersChinese(input.CurrentMessage),
			"The LLM Agent crossed a failure boundary; the deterministic runner handled this request.",
			"LLM Agent 触发失败边界，本次请求已由确定性降级路径处理。",
		),
	})
	return output, nil
}

var _ PromptRenderingRunner = (*FallbackRunner)(nil)

type controlledFallbackError struct {
	reason string
}

func (e controlledFallbackError) Error() string {
	return e.reason
}

func fallbackReason(err error) string {
	if controlled, ok := err.(controlledFallbackError); ok {
		return controlled.reason
	}
	return "llm_unavailable"
}

func fallbackErrorSummary(err error) (string, string) {
	if err == nil {
		return "unknown", "unknown Agent fallback trigger"
	}
	if controlled, ok := err.(controlledFallbackError); ok {
		return "controlled_failure_boundary", "Failure Controller requested deterministic fallback: " + controlled.reason
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", "LLM Agent execution timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled", "LLM Agent execution was canceled"
	}
	message := err.Error()
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "eino react execution failed"):
		return "react_execution_failed", "Eino ReAct execution failed before producing a valid final answer"
	case strings.Contains(lower, "json"):
		return "invalid_json", "LLM Agent returned output that could not be parsed as required JSON"
	case strings.Contains(lower, "tool"):
		return "tool_calling_failed", "LLM Agent tool-calling loop failed"
	default:
		return "llm_unavailable", "LLM Agent primary path failed"
	}
}

func outputNeedsControlledFallback(output AgentOutput) (bool, string) {
	if output.Metadata != nil {
		if required, _ := output.Metadata["failure_controller_fallback_required"].(bool); required {
			if reason, _ := output.Metadata["failure_reason"].(string); reason != "" {
				return true, reason
			}
			return true, "agent_failure_boundary"
		}
		if parseSuccess, ok := output.Metadata["output_parse_success"].(bool); ok && !parseSuccess {
			return true, "invalid_final_json"
		}
	}
	for _, limitation := range output.Limitations {
		switch limitation.Code {
		case "AGENT_OUTPUT_PARSE_FAILED", "AGENT_CONSECUTIVE_TOOL_FAILURES", "AGENT_MAX_TOOL_CALLS_EXCEEDED", "AGENT_TOTAL_EXECUTION_TIMEOUT":
			return true, limitation.Code
		}
	}
	return false, ""
}
