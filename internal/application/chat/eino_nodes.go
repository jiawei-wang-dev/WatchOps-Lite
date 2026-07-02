package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/skills"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
)

type graphState struct {
	command            Command
	snapshot           session.ContextSnapshot
	memoryAvailable    bool
	longTermMemories   []string
	agentInput         agenteino.AgentInput
	renderedMessages   []*schema.Message
	promptRenderFailed bool
	agentOutput        agenteino.AgentOutput
}

func (s *Service) loadSessionContextGraphNode(
	ctx context.Context,
	command Command,
) (graphState, error) {
	snapshot, available := s.loadContext(ctx, command.SessionID)
	return graphState{
		command:          command,
		snapshot:         snapshot,
		memoryAvailable:  available,
		longTermMemories: []string{},
	}, nil
}

func loadLongTermMemoryGraphNode(
	_ context.Context,
	state graphState,
) (graphState, error) {
	// Durable Agent memory is intentionally not implemented yet. Keeping this
	// native graph node explicit makes the adapter boundary visible without
	// querying MySQL or implying a delivered feature.
	state.longTermMemories = []string{}
	return state, nil
}

func buildPromptInputGraphNode(
	_ context.Context,
	state graphState,
) (graphState, error) {
	state.agentInput = agenteino.AgentInput{
		SessionSummary:     state.snapshot.Summary,
		RecentMessages:     state.snapshot.RecentMessages,
		LongTermMemories:   state.longTermMemories,
		DiagnosticSkills:   diagnosticSkillCards(),
		RetrievedKnowledge: []string{},
		CurrentMessage:     state.command.Message,
		TimeContext:        state.command.TimeContext,
	}
	return state, nil
}

func (s *Service) renderPromptTemplateGraphNode(
	ctx context.Context,
	state graphState,
) (graphState, error) {
	renderer, ok := s.runner.(agenteino.PromptRenderingRunner)
	if !ok {
		return state, nil
	}
	messages, err := renderer.RenderPrompt(ctx, state.agentInput)
	if err != nil {
		// Run the normal runner in the next node so its existing deterministic
		// fallback policy remains authoritative for prompt-render failures.
		state.promptRenderFailed = true
		return state, nil
	}
	state.renderedMessages = messages
	return state, nil
}

func (s *Service) runReActAgentGraphNode(
	ctx context.Context,
	state graphState,
) (graphState, error) {
	var (
		output agenteino.AgentOutput
		err    error
	)
	prepared, supportsPrepared := s.runner.(agenteino.PromptRenderingRunner)
	if supportsPrepared && !state.promptRenderFailed {
		output, err = prepared.RunPrepared(
			ctx,
			state.agentInput,
			state.renderedMessages,
		)
	} else {
		output, err = s.runner.Run(ctx, state.agentInput)
	}
	if err != nil {
		return graphState{}, fmt.Errorf("%w: %v", ErrExecution, err)
	}
	state.agentOutput = output
	return state, nil
}

func collectToolEvidenceGraphNode(
	_ context.Context,
	state graphState,
) (graphState, error) {
	// Eino and deterministic runners already normalize evidence through the
	// unified Evidence model. This node marks the graph boundary and attaches
	// only application-level memory availability metadata.
	ensureAgentMetadata(&state.agentOutput)
	state.agentOutput.Metadata["session_memory_available"] = state.memoryAvailable
	return state, nil
}

func (s *Service) persistSessionMemoryGraphNode(
	ctx context.Context,
	state graphState,
) (graphState, error) {
	if !state.memoryAvailable {
		appendMemoryLimitation(&state.agentOutput)
		return state, nil
	}
	if err := s.persistContext(
		ctx,
		state.command,
		state.snapshot,
		state.agentOutput,
	); err != nil {
		runtimemetrics.IncSessionMemoryUnavailable()
		state.memoryAvailable = false
		state.agentOutput.Metadata["session_memory_available"] = false
		appendMemoryLimitation(&state.agentOutput)
	}
	return state, nil
}

func buildChatResponseGraphNode(
	ctx context.Context,
	state graphState,
) (Result, error) {
	traceID := observability.TraceID(ctx)
	if traceID != "" {
		state.agentOutput.Metadata["trace_id"] = traceID
	}
	return Result{
		RequestID: state.command.RequestID,
		SessionID: state.command.SessionID,
		Agent:     state.agentOutput,
		TraceID:   traceID,
	}, nil
}

func diagnosticSkillCards() []string {
	definitions := []skills.Skill{
		skills.MetricInspectionSkill(),
		skills.LogInvestigationSkill(),
		skills.TraceInspectionSkill(),
		skills.RunbookLookupSkill(),
		skills.CheckoutIncidentDiagnosisSkill(),
	}
	cards := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		cards = append(cards, fmt.Sprintf(
			"%s: %s Tools: %s.",
			definition.Name(),
			definition.Description(),
			strings.Join(definition.ToolNames(), ", "),
		))
	}
	return cards
}
