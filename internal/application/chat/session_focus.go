package chat

import (
	"context"
	"strings"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"go.opentelemetry.io/otel/attribute"
)

const (
	sessionFocusMessageLimit = 6
	sessionFocusMessageRunes = 300
	sessionFocusSummaryRunes = 600
)

func (s *Service) loadSessionFocus(
	ctx context.Context,
	sessionID string,
) (session.SessionFocus, bool) {
	ctx, span := observability.StartSpan(
		ctx,
		"session.load_focus",
		attribute.String("session_id", sessionID),
	)
	defer span.End()
	if s.store == nil {
		runtimemetrics.IncSessionMemoryUnavailable()
		span.SetAttributes(
			attribute.Bool("session_focus_available", false),
			attribute.String("error_code", "SESSION_FOCUS_UNAVAILABLE"),
		)
		return session.EmptyFocus(), false
	}

	focus := session.EmptyFocus()
	if store, ok := s.store.(session.FocusStore); ok {
		loaded, err := store.LoadFocus(ctx, sessionID)
		if err != nil {
			runtimemetrics.IncSessionMemoryUnavailable()
			span.SetAttributes(
				attribute.Bool("session_focus_available", false),
				attribute.String("error_code", "SESSION_FOCUS_LOAD_FAILED"),
			)
			observability.MarkError(span, "session focus load failed")
			return session.EmptyFocus(), false
		}
		focus = loaded
	}

	// Older sessions and Store implementations may not yet have a focus record.
	// Load only the bounded window, never the complete context or tool history.
	if len(focus.RecentMessages) == 0 {
		messages, err := s.store.GetRecentMessages(ctx, sessionID, sessionFocusMessageLimit)
		if err != nil {
			runtimemetrics.IncSessionMemoryUnavailable()
			span.SetAttributes(
				attribute.Bool("session_focus_available", false),
				attribute.String("error_code", "SESSION_FOCUS_LOAD_FAILED"),
			)
			return session.EmptyFocus(), false
		}
		focus.RecentMessages = messages
	}
	focus = boundSessionFocus(focus)
	span.SetAttributes(
		attribute.Bool("session_focus_available", true),
		attribute.Int("recent_message_count", len(focus.RecentMessages)),
	)
	return focus, true
}

func (s *Service) persistSessionFocus(
	ctx context.Context,
	state graphState,
	status string,
) error {
	store, ok := s.store.(session.FocusStore)
	if !ok {
		return nil
	}
	focus := state.sessionFocus
	if focus.KnownSlots == nil {
		focus.KnownSlots = map[string]string{}
	}
	mergeFocusSlots(focus.KnownSlots, state.intentDecision.KnownSlots)
	mergeFocusSlots(focus.KnownSlots, map[string]string{
		"service":    state.intentResult.Service,
		"symptom":    state.intentResult.Symptom,
		"trace_id":   state.intentResult.TraceID,
		"time_range": intentTimeRange(state.intentResult.TimeRange),
	})
	focus.LastIntent = string(state.intentResult.Intent)
	focus.CurrentService = focus.KnownSlots["service"]
	focus.CurrentSymptom = focus.KnownSlots["symptom"]
	focus.CurrentTraceID = focus.KnownSlots["trace_id"]
	focus.CurrentTimeRange = focus.KnownSlots["time_range"]
	focus.TurnStatus = status
	focus.UpdatedAt = s.now()
	focus.MissingSlots = append([]string{}, state.intentDecision.MissingRequired...)
	if status == session.TurnStatusClarify {
		focus.PendingQuestion = state.intentDecision.ClarifyQuestion
	} else {
		focus.PendingQuestion = ""
		focus.MissingSlots = []string{}
	}
	focus.Candidates = mergeCandidates(focus.Candidates, extractServiceCandidates(state.command.Message))
	focus.EvidenceIDs = evidenceIDsOnly(state.agentOutput)
	focus.Summary = truncateFocusText(assistantMemoryContent(state.agentOutput), sessionFocusSummaryRunes)
	focus.RecentMessages = append(focus.RecentMessages,
		session.Message{
			Role:      session.RoleUser,
			Content:   state.command.Message,
			CreatedAt: s.now(),
			RequestID: state.command.RequestID,
		},
		session.Message{
			Role:      session.RoleAssistant,
			Content:   assistantMemoryContent(state.agentOutput),
			CreatedAt: s.now(),
			RequestID: state.command.RequestID,
		},
	)
	focus = boundSessionFocus(focus)

	ctx, span := observability.StartSpan(
		ctx,
		"session.persist_focus",
		attribute.String("session_id", state.command.SessionID),
		attribute.String("turn_status", status),
	)
	defer span.End()
	if err := store.SaveFocus(ctx, state.command.SessionID, focus); err != nil {
		span.SetAttributes(attribute.String("error_code", "SESSION_FOCUS_PERSIST_FAILED"))
		observability.MarkError(span, "session focus persistence failed")
		return err
	}
	return nil
}

func boundSessionFocus(focus session.SessionFocus) session.SessionFocus {
	focus.KnownSlots = cloneStringMap(focus.KnownSlots)
	focus.MissingSlots = append([]string{}, focus.MissingSlots...)
	if len(focus.RecentMessages) > sessionFocusMessageLimit {
		focus.RecentMessages = focus.RecentMessages[len(focus.RecentMessages)-sessionFocusMessageLimit:]
	}
	bounded := make([]session.Message, 0, len(focus.RecentMessages))
	for _, message := range focus.RecentMessages {
		if message.Role == session.RoleTool || message.Role == session.RoleSystem {
			continue
		}
		message.Content = truncateFocusText(redactFocusText(message.Content), sessionFocusMessageRunes)
		message.Metadata = nil
		bounded = append(bounded, message)
	}
	focus.RecentMessages = bounded
	focus.Summary = truncateFocusText(redactFocusText(focus.Summary), sessionFocusSummaryRunes)
	focus.PendingQuestion = truncateFocusText(redactFocusText(focus.PendingQuestion), sessionFocusMessageRunes)
	focus.EvidenceIDs = boundedStrings(focus.EvidenceIDs, 20, 128)
	focus.Candidates = boundedStrings(focus.Candidates, 10, 128)
	return focus
}

func truncateFocusText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func boundedStrings(values []string, count, runes int) []string {
	if len(values) > count {
		values = values[:count]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = truncateFocusText(value, runes); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func mergeFocusSlots(target, source map[string]string) {
	for key, value := range source {
		if value = strings.TrimSpace(value); value != "" {
			target[key] = value
		}
	}
}

func intentTimeRange(value *intent.TimeRangeHint) string {
	if value == nil {
		return ""
	}
	if value.Relative != "" {
		return value.Relative
	}
	if value.From != "" || value.To != "" {
		return value.From + "/" + value.To
	}
	return ""
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

func extractServiceCandidates(message string) []string {
	lower := strings.ToLower(message)
	candidates := []string{}
	for _, name := range []string{
		"checkout", "payment", "order", "cart", "catalog", "frontend",
		"backend", "api", "auth", "inventory", "shipping",
	} {
		if strings.Contains(lower, name) {
			candidates = append(candidates, name)
		}
	}
	return candidates
}

func mergeCandidates(existing, current []string) []string {
	if len(current) > 0 {
		return boundedStrings(current, 10, 128)
	}
	return boundedStrings(existing, 10, 128)
}
