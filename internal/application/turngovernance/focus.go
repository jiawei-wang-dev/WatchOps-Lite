package turngovernance

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"go.opentelemetry.io/otel/attribute"
)

const (
	focusMessageLimit = 6
	focusMessageRunes = 300
	focusSummaryRunes = 600
)

var focusSecretPattern = regexp.MustCompile(
	`(?i)(api[_-]?key|password|secret|token|authorization|cookie|redis://|mysql://|postgres://)\s*[:=]?\s*\S+`,
)

// LoadFocus loads only the bounded pre-intent session state. It deliberately
// does not call LoadContext, so full history and raw tool messages cannot enter
// intent recognition.
func LoadFocus(
	ctx context.Context,
	store session.Store,
	sessionID string,
) (session.SessionFocus, bool) {
	ctx, span := observability.StartSpan(
		ctx,
		"focus.load",
		attribute.String("session_id", sessionID),
	)
	defer span.End()
	if store == nil {
		markFocusUnavailable(span, "SESSION_FOCUS_UNAVAILABLE")
		return session.EmptyFocus(), false
	}

	focus := session.EmptyFocus()
	if focusStore, ok := store.(session.FocusStore); ok {
		loaded, err := focusStore.LoadFocus(ctx, sessionID)
		if err != nil {
			markFocusUnavailable(span, "SESSION_FOCUS_LOAD_FAILED")
			observability.MarkError(span, "session focus load failed")
			return session.EmptyFocus(), false
		}
		focus = loaded
	}
	if len(focus.RecentMessages) == 0 {
		messages, err := store.GetRecentMessages(ctx, sessionID, focusMessageLimit)
		if err != nil {
			markFocusUnavailable(span, "SESSION_FOCUS_LOAD_FAILED")
			observability.MarkError(span, "bounded session messages load failed")
			return session.EmptyFocus(), false
		}
		focus.RecentMessages = messages
	}
	focus = BoundFocus(focus)
	span.SetAttributes(
		attribute.Bool("session_focus_available", true),
		attribute.Int("recent_message_count", len(focus.RecentMessages)),
		attribute.Int64("focus.version", focus.Version),
	)
	return focus, true
}

func markFocusUnavailable(span interface {
	SetAttributes(...attribute.KeyValue)
}, code string) {
	runtimemetrics.IncSessionMemoryUnavailable()
	span.SetAttributes(
		attribute.Bool("session_focus_available", false),
		attribute.String("error_code", code),
	)
}

// FocusUpdate contains only bounded governance state. Callers pass IDs and a
// short assistant summary, never raw tool output, prompts, or span payloads.
type FocusUpdate struct {
	SessionID        string
	RequestID        string
	Message          string
	AssistantSummary string
	Status           string
	Focus            session.SessionFocus
	Decision         intent.IntentDecision
	ResolvedIntent   intent.IntentResult
	EvidenceIDs      []string
	Now              time.Time
}

func PersistFocus(
	ctx context.Context,
	store session.Store,
	update FocusUpdate,
) error {
	focusStore, ok := store.(session.FocusStore)
	if !ok {
		return nil
	}
	focus := update.Focus
	if focus.KnownSlots == nil {
		focus.KnownSlots = map[string]string{}
	}
	mergeSlots(focus.KnownSlots, update.Decision.KnownSlots)
	mergeSlots(focus.KnownSlots, ResultSlots(update.ResolvedIntent))
	focus.LastIntent = string(update.ResolvedIntent.Intent)
	focus.CurrentService = focus.KnownSlots["service"]
	focus.CurrentSymptom = focus.KnownSlots["symptom"]
	focus.CurrentTraceID = focus.KnownSlots["trace_id"]
	focus.CurrentTimeRange = focus.KnownSlots["time_range"]
	focus.TurnStatus = update.Status
	focus.UpdatedAt = update.Now.UTC()
	focus.MissingSlots = append([]string{}, update.Decision.MissingRequired...)
	if update.Status == session.TurnStatusClarify {
		focus.PendingQuestion = update.Decision.ClarifyQuestion
	} else {
		focus.PendingQuestion = ""
		focus.MissingSlots = []string{}
	}
	focus.Candidates = MergeCandidates(
		focus.Candidates,
		ExtractServiceCandidates(update.Message),
	)
	focus.EvidenceIDs = append([]string{}, update.EvidenceIDs...)
	focus.Summary = update.AssistantSummary
	focus.RecentMessages = append(focus.RecentMessages, session.Message{
		Role:      session.RoleUser,
		Content:   update.Message,
		CreatedAt: update.Now,
		RequestID: update.RequestID,
	})
	if strings.TrimSpace(update.AssistantSummary) != "" {
		focus.RecentMessages = append(focus.RecentMessages, session.Message{
			Role:      session.RoleAssistant,
			Content:   update.AssistantSummary,
			CreatedAt: update.Now,
			RequestID: update.RequestID,
		})
	}
	focus = BoundFocus(focus)

	ctx, span := observability.StartSpan(
		ctx,
		"focus.persist",
		attribute.String("session_id", update.SessionID),
		attribute.String("turn_status", update.Status),
		attribute.Int64("focus.expected_version", focus.Version),
	)
	defer span.End()
	if err := focusStore.SaveFocus(ctx, update.SessionID, focus); err != nil {
		span.SetAttributes(attribute.String("error_code", "SESSION_FOCUS_PERSIST_FAILED"))
		observability.MarkError(span, "session focus persistence failed")
		return err
	}
	return nil
}

func BoundFocus(focus session.SessionFocus) session.SessionFocus {
	focus.KnownSlots = CloneStringMap(focus.KnownSlots)
	focus.MissingSlots = append([]string{}, focus.MissingSlots...)
	if len(focus.RecentMessages) > focusMessageLimit {
		focus.RecentMessages = focus.RecentMessages[len(focus.RecentMessages)-focusMessageLimit:]
	}
	bounded := make([]session.Message, 0, len(focus.RecentMessages))
	for _, message := range focus.RecentMessages {
		if message.Role == session.RoleTool || message.Role == session.RoleSystem {
			continue
		}
		message.Content = truncate(RedactText(message.Content), focusMessageRunes)
		message.Metadata = nil
		bounded = append(bounded, message)
	}
	focus.RecentMessages = bounded
	focus.Summary = truncate(RedactText(focus.Summary), focusSummaryRunes)
	focus.PendingQuestion = truncate(RedactText(focus.PendingQuestion), focusMessageRunes)
	focus.EvidenceIDs = BoundedStrings(focus.EvidenceIDs, 20, 128)
	focus.Candidates = BoundedStrings(focus.Candidates, 10, 128)
	return focus
}

func FocusView(focus session.SessionFocus, available bool) intent.FocusView {
	return intent.FocusView{
		LastIntent:      focus.LastIntent,
		KnownSlots:      CloneStringMap(focus.KnownSlots),
		PendingQuestion: focus.PendingQuestion,
		Candidates:      append([]string{}, focus.Candidates...),
		TurnStatus:      focus.TurnStatus,
		Summary:         focus.Summary,
		Available:       available,
	}
}

func RecentMessageViews(messages []session.Message) []intent.MessageView {
	result := make([]intent.MessageView, 0, len(messages))
	for _, message := range messages {
		if message.Role == session.RoleTool || message.Role == session.RoleSystem {
			continue
		}
		result = append(result, intent.MessageView{
			Role:      string(message.Role),
			Content:   RedactText(message.Content),
			CreatedAt: message.CreatedAt,
		})
	}
	return intent.BoundedMessages(result)
}

func ResultSlots(result intent.IntentResult) map[string]string {
	slots := map[string]string{
		"service":  result.Service,
		"symptom":  result.Symptom,
		"trace_id": result.TraceID,
	}
	if result.TimeRange != nil {
		if result.TimeRange.Relative != "" {
			slots["time_range"] = result.TimeRange.Relative
		} else if result.TimeRange.From != "" || result.TimeRange.To != "" {
			slots["time_range"] = result.TimeRange.From + "/" + result.TimeRange.To
		}
	}
	return slots
}

func CloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func ExtractServiceCandidates(message string) []string {
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

func MergeCandidates(existing, current []string) []string {
	if len(current) > 0 {
		return BoundedStrings(current, 10, 128)
	}
	return BoundedStrings(existing, 10, 128)
}

func BoundedStrings(values []string, count, runes int) []string {
	if len(values) > count {
		values = values[:count]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = truncate(value, runes); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func RedactText(value string) string {
	return focusSecretPattern.ReplaceAllString(strings.TrimSpace(value), "[REDACTED]")
}

func truncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func mergeSlots(target, source map[string]string) {
	for key, value := range source {
		if value = strings.TrimSpace(value); value != "" {
			target[key] = value
		}
	}
}
