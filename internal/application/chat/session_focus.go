package chat

import (
	"context"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/turngovernance"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
)

func (s *Service) loadSessionFocus(
	ctx context.Context,
	sessionID string,
) (session.SessionFocus, bool) {
	return turngovernance.LoadFocus(ctx, s.store, sessionID)
}

func (s *Service) persistSessionFocus(
	ctx context.Context,
	state graphState,
	status string,
) error {
	return turngovernance.PersistFocus(ctx, s.store, turngovernance.FocusUpdate{
		SessionID:        state.command.SessionID,
		RequestID:        state.command.RequestID,
		Message:          state.command.Message,
		AssistantSummary: assistantMemoryContent(state.agentOutput),
		Status:           status,
		Focus:            state.sessionFocus,
		Decision:         state.intentDecision,
		ResolvedIntent:   state.intentResult,
		EvidenceIDs:      evidenceIDsOnly(state.agentOutput),
		Now:              s.now(),
	})
}

// These small wrappers preserve the Single-Agent graph's existing test and
// node boundaries while both agent modes share the governance implementation.
func boundSessionFocus(focus session.SessionFocus) session.SessionFocus {
	return turngovernance.BoundFocus(focus)
}

func evidenceIDsOnly(output agenteino.AgentOutput) []string {
	result := make([]string, 0, len(output.Evidence))
	for _, item := range output.Evidence {
		if item.ID != "" {
			result = append(result, item.ID)
		}
	}
	return result
}
