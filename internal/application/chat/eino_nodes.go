package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/skills"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
)

type graphState struct {
	command                   Command
	snapshot                  session.ContextSnapshot
	memoryAvailable           bool
	longTermMemories          []longterm.Memory
	longTermMemoryUnavailable bool
	agentInput                agenteino.AgentInput
	renderedMessages          []*schema.Message
	promptRenderFailed        bool
	agentOutput               agenteino.AgentOutput
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
		longTermMemories: []longterm.Memory{},
	}, nil
}

func (s *Service) loadLongTermMemoryGraphNode(
	ctx context.Context,
	state graphState,
) (graphState, error) {
	if s.longTermMemory == nil {
		return state, nil
	}
	memories, err := s.longTermMemory.Search(ctx, longterm.SearchQuery{
		Query: state.command.Message,
		Limit: s.longTermMemoryTopK,
	})
	if err != nil {
		state.longTermMemoryUnavailable = true
		state.longTermMemories = []longterm.Memory{}
		return state, nil
	}
	state.longTermMemories = memories
	return state, nil
}

func buildPromptInputGraphNode(
	_ context.Context,
	state graphState,
) (graphState, error) {
	state.agentInput = agenteino.AgentInput{
		SessionSummary: state.snapshot.Summary,
		RecentMessages: state.snapshot.RecentMessages,
		ConfirmedLongTermMemories: confirmedLongTermMemoryPrompt(
			state.longTermMemories,
		),
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
	state.agentOutput.Metadata["long_term_memory_count"] = len(state.longTermMemories)
	if state.longTermMemoryUnavailable {
		state.agentOutput.Limitations = append(
			state.agentOutput.Limitations,
			agenteino.Limitation{
				Code:    "LONG_TERM_MEMORY_UNAVAILABLE",
				Message: "Confirmed long-term memory is unavailable; this response was generated without cross-session incident context.",
			},
		)
	}
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
			"%s: %s",
			definition.Name(),
			definition.Description(),
		))
	}
	return cards
}

func confirmedLongTermMemoryPrompt(memories []longterm.Memory) []string {
	result := make([]string, 0, len(memories))
	for _, memory := range memories {
		summary := truncatePromptText(memory.Summary, 500)
		if summary == "" {
			continue
		}
		value := memory.Title + ": " + summary
		if memory.Service != "" {
			value += " [service=" + memory.Service + "]"
		}
		if len(memory.EvidenceIDs) > 0 {
			value += " [evidence_ids=" + strings.Join(memory.EvidenceIDs, ",") + "]"
		}
		result = append(result, value)
	}
	return result
}

func truncatePromptText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
