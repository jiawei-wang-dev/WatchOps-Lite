package turngovernance

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

type TurnInput struct {
	RequestID      string
	SessionID      string
	UserID         string
	Message        string
	TimeContext    common.TimeRange
	Metadata       map[string]any
	ProvidedIntent intent.IntentResult
}

type TurnOutcome struct {
	Decision        intent.IntentDecision
	Focus           session.SessionFocus
	ResolvedIntent  intent.IntentResult
	ResolvedTime    common.TimeRange
	MemoryAvailable bool
}

type Resolver struct {
	store         session.Store
	recognizer    intent.Recognizer
	minConfidence float64
	now           func() time.Time
}

func NewResolver(
	store session.Store,
	recognizer intent.Recognizer,
	minConfidence float64,
) *Resolver {
	if recognizer == nil {
		recognizer = intent.NewRuleBasedRecognizer()
	}
	if minConfidence <= 0 {
		minConfidence = 0.55
	}
	return &Resolver{
		store:         store,
		recognizer:    recognizer,
		minConfidence: minConfidence,
		now:           func() time.Time { return time.Now().UTC() },
	}
}

func (r *Resolver) WithNow(now func() time.Time) *Resolver {
	if now != nil {
		r.now = now
	}
	return r
}

// Resolve is the shared execution gate for all agent modes. It decides whether
// the turn is executable before any role planning, RAG, agent, or tool work.
func (r *Resolver) Resolve(
	ctx context.Context,
	input TurnInput,
) (TurnOutcome, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"turn_governance.resolve",
		attribute.String("request_id", input.RequestID),
		attribute.String("session_id", input.SessionID),
	)
	defer span.End()

	now := r.now()
	focus, available := LoadFocus(ctx, r.store, input.SessionID)
	if !available {
		_, degradedSpan := observability.StartSpan(
			ctx,
			"governance.degraded",
			attribute.String("error_code", "SESSION_FOCUS_UNAVAILABLE"),
		)
		degradedSpan.End()
	}
	result := input.ProvidedIntent
	if result.Intent == "" {
		recognizeCtx, recognizeSpan := observability.StartSpan(
			ctx,
			"intent.recognize",
			attribute.Bool("session_focus_available", available),
		)
		recognized, err := r.recognizer.Recognize(recognizeCtx, RecognitionInput(
			input,
			focus,
			available,
			now,
		))
		recognizeSpan.End()
		if err != nil {
			result = intent.SafeDefault(input.Message, intent.IntentLimitation{
				Code:    "INTENT_RECOGNITION_FAILED",
				Message: "Intent recognition failed; safe default intent was used.",
			})
		} else {
			result = recognized
		}
	}
	result = intent.Normalize(result)
	result.Metadata["session_focus_available"] = available
	agenteino.EmitStreamEvent(ctx, "intent_recognized", map[string]any{
		"intent":           result.Intent,
		"confidence":       result.Confidence,
		"source":           result.Source,
		"suggested_tools":  result.SuggestedTools,
		"suggested_agents": result.SuggestedAgents,
		"fallback_used":    result.Metadata["fallback_used"],
	})

	_, validateSpan := observability.StartSpan(
		ctx,
		"slot.validate",
		attribute.String("intent.type", string(result.Intent)),
	)
	decision := intent.ValidateSlots(
		input.Message,
		result,
		CommandSlots(input.TimeContext),
		FocusView(focus, available),
		r.minConfidence,
	)
	validateSpan.SetAttributes(
		attribute.String("slot.decision", string(decision.Decision)),
		attribute.String("slot.reason_code", decision.ReasonCode),
	)
	validateSpan.End()
	agenteino.EmitStreamEvent(ctx, "slot_validation_completed", map[string]any{
		"decision":               decision.Decision,
		"reason_code":            decision.ReasonCode,
		"missing_required_slots": decision.MissingRequired,
		"known_slots":            decision.KnownSlots,
	})
	span.SetAttributes(
		attribute.String("intent.type", string(decision.Result.Intent)),
		attribute.String("slot.decision", string(decision.Decision)),
		attribute.Bool("session_focus_available", available),
	)
	if decision.Decision == intent.DecisionClarify {
		_, clarifySpan := observability.StartSpan(
			ctx,
			"clarification.required",
			attribute.String("reason_code", decision.ReasonCode),
			attribute.StringSlice("missing_slots", decision.MissingRequired),
		)
		clarifySpan.End()
	}
	return TurnOutcome{
		Decision:        decision,
		Focus:           focus,
		ResolvedIntent:  decision.Result,
		ResolvedTime:    ResolveTimeContext(input.TimeContext, decision.Result.TimeRange, now),
		MemoryAvailable: available,
	}, nil
}

func RecognitionInput(
	input TurnInput,
	focus session.SessionFocus,
	available bool,
	now time.Time,
) intent.RecognitionInput {
	return intent.RecognitionInput{
		Message:        input.Message,
		SessionID:      input.SessionID,
		UserID:         input.UserID,
		Now:            now,
		RecentMessages: RecentMessageViews(focus.RecentMessages),
		Focus:          FocusView(focus, available),
		Metadata: map[string]any{
			"session_focus_available": available,
			"last_intent":             focus.LastIntent,
			"known_slots":             CloneStringMap(focus.KnownSlots),
			"pending_question":        focus.PendingQuestion,
			"turn_status":             focus.TurnStatus,
		},
		AvailableTools: []string{
			"query_metrics", "query_logs", "query_traces", "search_knowledge",
		},
		AvailableSkills: []string{
			"metric_inspection",
			"log_investigation",
			"trace_inspection",
			"runbook_lookup",
			"checkout_incident_diagnosis",
		},
	}
}

func CommandSlots(value common.TimeRange) map[string]string {
	result := map[string]string{}
	if value.From != "" || value.To != "" {
		result["time_range"] = value.From + "/" + value.To
	}
	return result
}

var relativeMinutesPattern = regexp.MustCompile(`^last_(\d+)_minutes$`)

func ResolveTimeContext(
	fallback common.TimeRange,
	hint *intent.TimeRangeHint,
	now time.Time,
) common.TimeRange {
	if hint == nil {
		return fallback
	}
	if strings.TrimSpace(hint.From) != "" || strings.TrimSpace(hint.To) != "" {
		return common.TimeRange{
			From: strings.TrimSpace(hint.From),
			To:   strings.TrimSpace(hint.To),
		}
	}
	relative := strings.TrimSpace(hint.Relative)
	if bounds := strings.SplitN(relative, "/", 2); len(bounds) == 2 {
		if _, fromErr := time.Parse(time.RFC3339, bounds[0]); fromErr == nil {
			if _, toErr := time.Parse(time.RFC3339, bounds[1]); toErr == nil {
				return common.TimeRange{From: bounds[0], To: bounds[1]}
			}
		}
	}
	now = now.UTC()
	format := func(value time.Time) string { return value.Format(time.RFC3339) }
	if match := relativeMinutesPattern.FindStringSubmatch(relative); len(match) == 2 {
		minutes, err := strconv.Atoi(match[1])
		if err == nil && minutes > 0 {
			return common.TimeRange{
				From: format(now.Add(-time.Duration(minutes) * time.Minute)),
				To:   format(now),
			}
		}
	}
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch relative {
	case "yesterday":
		return common.TimeRange{
			From: format(startToday.AddDate(0, 0, -1)),
			To:   format(startToday),
		}
	case "today":
		return common.TimeRange{From: format(startToday), To: format(now)}
	case "last_week":
		return common.TimeRange{From: format(now.AddDate(0, 0, -7)), To: format(now)}
	default:
		return fallback
	}
}
