package control

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type Limitation struct {
	Code    string
	Message string
	Tool    string
}

type Evaluation struct {
	FailureReason  string
	ShouldFallback bool
	Controlled     bool
	Limitations    []Limitation
}

type Controller struct {
	config Config
}

func New(config Config) *Controller {
	return &Controller{config: Normalize(config)}
}

func (c *Controller) Config() Config {
	return c.config
}

func (c *Controller) Evaluate(ctx context.Context, state State) Evaluation {
	ctx, span := observability.StartSpan(
		ctx,
		"agent.failure_controller.evaluate",
		attribute.Int("tool_failure_count", state.FailedToolCount),
		attribute.Int("evidence_count", state.EvidenceCount),
		attribute.Int("tool_call_count", state.ToolCallCount),
		attribute.Int("consecutive_tool_failures", state.ConsecutiveToolFailures),
		attribute.Int64("elapsed_ms", state.Elapsed.Milliseconds()),
	)
	defer span.End()

	evaluation := Evaluation{Limitations: []Limitation{}}
	addControlled := func(reason string, limitation Limitation, fallback bool) {
		if evaluation.FailureReason == "" {
			evaluation.FailureReason = reason
		}
		evaluation.Controlled = true
		evaluation.ShouldFallback = evaluation.ShouldFallback || fallback
		evaluation.Limitations = append(evaluation.Limitations, limitation)
	}

	if !state.OutputParseSuccess {
		addControlled("invalid_final_json", Limitation{
			Code:    "AGENT_OUTPUT_PARSE_FAILED",
			Message: "The model response could not be parsed into the required answer structure.",
		}, true)
	}
	if state.MissingRequiredSections {
		addControlled("missing_required_sections", Limitation{
			Code:    "AGENT_OUTPUT_MISSING_REQUIRED_SECTIONS",
			Message: "The model response omitted one or more required answer sections.",
		}, false)
	}
	if state.ToolCallCount > c.config.MaxToolCalls {
		addControlled("max_tool_calls_exceeded", Limitation{
			Code:    "AGENT_MAX_TOOL_CALLS_EXCEEDED",
			Message: "The Agent exceeded the configured maximum number of tool calls.",
		}, true)
	}
	if state.ConsecutiveToolFailures >= c.config.MaxConsecutiveToolFailures {
		addControlled("consecutive_tool_failures", Limitation{
			Code:    "AGENT_CONSECUTIVE_TOOL_FAILURES",
			Message: "Several tools failed consecutively, so the Agent stopped trusting further risky execution.",
		}, true)
	}
	if c.config.EnableRepeatedToolDetection && state.RepeatedToolCallCount > 1 {
		addControlled("repeated_tool_call", Limitation{
			Code:    "AGENT_REPEATED_TOOL_CALL",
			Message: "The Agent repeated the same tool call pattern and the duplicate execution was treated as low value.",
		}, false)
	}
	if state.MaxIterationsReachedLikely {
		addControlled("max_iterations_reached", Limitation{
			Code:    "AGENT_MAX_ITERATIONS_REACHED",
			Message: "The Agent reached the configured iteration boundary before producing stronger evidence.",
		}, false)
	}
	if state.Elapsed > c.config.TotalExecutionTimeout {
		addControlled("total_execution_timeout", Limitation{
			Code:    "AGENT_TOTAL_EXECUTION_TIMEOUT",
			Message: "The Agent exceeded the configured total execution timeout.",
		}, true)
	}
	if state.EvidenceCount == 0 {
		addControlled("empty_evidence", Limitation{
			Code:    "INSUFFICIENT_EVIDENCE",
			Message: "No tool evidence was returned to support an observed root-cause claim.",
		}, false)
	}

	span.SetAttributes(
		attribute.String("failure_reason", evaluation.FailureReason),
		attribute.Bool("controlled", evaluation.Controlled),
		attribute.Bool("fallback_required", evaluation.ShouldFallback),
	)
	return evaluation
}

func (c *Controller) RepairJSON(ctx context.Context, content string) (string, bool) {
	ctx, span := observability.StartSpan(
		ctx,
		"agent.failure_controller.json_repair",
		attribute.Bool("enabled", c.config.EnableJSONRepairOnce),
		attribute.Int("content_length", len(content)),
	)
	defer span.End()
	if !c.config.EnableJSONRepairOnce {
		span.SetAttributes(attribute.Bool("repair_success", false))
		return content, false
	}
	if validJSON(content) {
		span.SetAttributes(attribute.Bool("repair_success", false))
		return content, false
	}
	repaired := stripToJSONObject(content)
	repaired = removeTrailingCommas(repaired)
	if !validJSON(repaired) {
		span.SetAttributes(attribute.Bool("repair_success", false))
		return content, false
	}
	span.SetAttributes(attribute.Bool("repair_success", true))
	return repaired, true
}

func (c *Controller) MarkFallback(ctx context.Context, reason string) {
	_, span := observability.StartSpan(
		ctx,
		"agent.failure_controller.fallback",
		attribute.String("failure_reason", reason),
	)
	span.End()
}

func validJSON(content string) bool {
	var raw json.RawMessage
	return json.Unmarshal([]byte(strings.TrimSpace(content)), &raw) == nil
}

func stripToJSONObject(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if newline := strings.IndexByte(content, '\n'); newline >= 0 {
			content = content[newline+1:]
		}
		content = strings.TrimSpace(strings.TrimSuffix(content, "```"))
	}
	start := strings.IndexByte(content, '{')
	end := strings.LastIndexByte(content, '}')
	if start < 0 || end < start {
		return content
	}
	return strings.TrimSpace(content[start : end+1])
}

func removeTrailingCommas(content string) string {
	var builder strings.Builder
	builder.Grow(len(content))
	inString := false
	escaped := false
	for index, current := range content {
		if escaped {
			builder.WriteRune(current)
			escaped = false
			continue
		}
		if current == '\\' && inString {
			builder.WriteRune(current)
			escaped = true
			continue
		}
		if current == '"' {
			inString = !inString
			builder.WriteRune(current)
			continue
		}
		if current == ',' && !inString {
			next := nextNonSpace(content[index+len(string(current)):])
			if next == '}' || next == ']' {
				continue
			}
		}
		builder.WriteRune(current)
	}
	return builder.String()
}

func nextNonSpace(value string) rune {
	for _, current := range value {
		if current != ' ' && current != '\n' && current != '\r' && current != '\t' {
			return current
		}
	}
	return 0
}

func Since(startedAt time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	return time.Since(startedAt)
}
