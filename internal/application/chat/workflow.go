package chat

import (
	"context"
	"fmt"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"go.opentelemetry.io/otel/attribute"
)

// workflowState is internal request state passed through the explicit Chat
// workflow. It is deliberately not a second Agent state machine: Eino ReAct
// remains responsible for model and tool orchestration.
type workflowState struct {
	command         Command
	snapshot        session.ContextSnapshot
	memoryAvailable bool
	agentInput      agenteino.AgentInput
	agentOutput     agenteino.AgentOutput
}

func (s *Service) executeWorkflow(ctx context.Context, command Command) (Result, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"workflow.chat",
		attribute.String("request_id", command.RequestID),
		attribute.String("session_id", command.SessionID),
	)
	defer span.End()

	state := workflowState{command: command}
	state.snapshot, state.memoryAvailable = s.loadContextNode(ctx, command.SessionID)
	span.SetAttributes(
		attribute.Bool("session_memory_available", state.memoryAvailable),
		attribute.Int("recent_message_count", len(state.snapshot.RecentMessages)),
		attribute.Int64("summary_version", state.snapshot.Summary.Version),
	)
	state.agentInput = buildAgentInputNode(ctx, state)

	output, err := s.runReActAgentNode(ctx, state.agentInput)
	if err != nil {
		observability.MarkError(span, "agent execution failed")
		return Result{}, fmt.Errorf("%w: %v", ErrExecution, err)
	}
	state.agentOutput = collectEvidenceNode(ctx, output, state.memoryAvailable)

	if err := s.persistMemoryNode(ctx, state); err != nil {
		runtimemetrics.IncSessionMemoryUnavailable()
		state.memoryAvailable = false
		state.agentOutput.Metadata["session_memory_available"] = false
		appendMemoryLimitation(&state.agentOutput)
	} else if !state.memoryAvailable {
		appendMemoryLimitation(&state.agentOutput)
	}

	return buildResponseNode(ctx, state), nil
}

func (s *Service) loadContextNode(
	ctx context.Context,
	sessionID string,
) (session.ContextSnapshot, bool) {
	ctx, span := observability.StartSpan(
		ctx,
		"graph.load_context",
		attribute.String("session_id", sessionID),
	)
	defer span.End()

	snapshot, available := s.loadContext(ctx, sessionID)
	span.SetAttributes(
		attribute.Bool("memory_available", available),
		attribute.Int("recent_message_count", len(snapshot.RecentMessages)),
	)
	return snapshot, available
}

func buildAgentInputNode(ctx context.Context, state workflowState) agenteino.AgentInput {
	_, span := observability.StartSpan(
		ctx,
		"graph.build_agent_input",
		attribute.Int("recent_message_count", len(state.snapshot.RecentMessages)),
		attribute.Int64("summary_version", state.snapshot.Summary.Version),
	)
	defer span.End()

	return agenteino.AgentInput{
		SessionSummary: state.snapshot.Summary,
		RecentMessages: state.snapshot.RecentMessages,
		CurrentMessage: state.command.Message,
		TimeContext:    state.command.TimeContext,
	}
}

func (s *Service) runReActAgentNode(
	ctx context.Context,
	input agenteino.AgentInput,
) (agenteino.AgentOutput, error) {
	ctx, span := observability.StartSpan(ctx, "graph.run_react_agent")
	defer span.End()

	output, err := s.runner.Run(ctx, input)
	if err != nil {
		observability.MarkError(span, "ReAct Agent execution failed")
		return agenteino.AgentOutput{}, err
	}
	span.SetAttributes(
		attribute.Int("tool_run_count", len(output.ToolRuns)),
		attribute.Int("evidence_count", len(output.Evidence)),
	)
	return output, nil
}

func collectEvidenceNode(
	ctx context.Context,
	output agenteino.AgentOutput,
	memoryAvailable bool,
) agenteino.AgentOutput {
	_, span := observability.StartSpan(
		ctx,
		"graph.collect_evidence",
		attribute.Int("evidence_count", len(output.Evidence)),
		attribute.Int("tool_run_count", len(output.ToolRuns)),
	)
	defer span.End()

	// Both Eino and deterministic runners already normalize and validate
	// evidence. This node marks that boundary without mutating the schema.
	ensureAgentMetadata(&output)
	output.Metadata["session_memory_available"] = memoryAvailable
	return output
}

func (s *Service) persistMemoryNode(ctx context.Context, state workflowState) error {
	ctx, span := observability.StartSpan(
		ctx,
		"graph.persist_memory",
		attribute.String("session_id", state.command.SessionID),
	)
	defer span.End()

	if !state.memoryAvailable {
		span.SetAttributes(attribute.Bool("skipped", true))
		return nil
	}
	if err := s.persistContext(
		ctx,
		state.command,
		state.snapshot,
		state.agentOutput,
	); err != nil {
		observability.MarkError(span, "session memory persistence failed")
		return err
	}
	return nil
}

func buildResponseNode(ctx context.Context, state workflowState) Result {
	_, span := observability.StartSpan(
		ctx,
		"graph.build_response",
		attribute.Int("evidence_count", len(state.agentOutput.Evidence)),
		attribute.Int("limitation_count", len(state.agentOutput.Limitations)),
	)
	defer span.End()

	traceID := observability.TraceID(ctx)
	if traceID != "" {
		state.agentOutput.Metadata["trace_id"] = traceID
	}
	return Result{
		RequestID: state.command.RequestID,
		SessionID: state.command.SessionID,
		Agent:     state.agentOutput,
		TraceID:   traceID,
	}
}
