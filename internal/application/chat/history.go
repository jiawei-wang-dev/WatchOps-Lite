package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultHistoryLimit = 20
	maxHistoryLimit     = 100
)

var ErrSessionMemoryUnavailable = errors.New("session memory unavailable")

type HistoryQuery struct {
	SessionID string
	Limit     int
}

type HistoryResult struct {
	SessionID string
	Summary   session.Summary
	Messages  []session.Message
	Limit     int
}

func (s *Service) GetHistory(
	ctx context.Context,
	query HistoryQuery,
) (HistoryResult, error) {
	sessionID, limit, err := validateHistoryQuery(query)
	if err != nil {
		return HistoryResult{}, err
	}
	ctx, span := observability.StartSpan(
		ctx,
		"session.history.get",
		attribute.String("session_id", sessionID),
		attribute.Int("limit", limit),
	)
	defer span.End()

	if s.store == nil {
		observability.MarkError(span, "session history unavailable")
		return HistoryResult{}, ErrSessionMemoryUnavailable
	}
	summary, err := s.store.GetSummary(ctx, sessionID)
	if err != nil {
		observability.MarkError(span, "session history summary read failed")
		return HistoryResult{}, fmt.Errorf("%w: read summary", ErrSessionMemoryUnavailable)
	}
	messages, err := s.store.GetRecentMessages(ctx, sessionID, limit)
	if err != nil {
		observability.MarkError(span, "session history message read failed")
		return HistoryResult{}, fmt.Errorf("%w: read messages", ErrSessionMemoryUnavailable)
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	if messages == nil {
		messages = []session.Message{}
	}
	span.SetAttributes(attribute.Int("message_count", len(messages)))
	return HistoryResult{
		SessionID: sessionID,
		Summary:   summary,
		Messages:  messages,
		Limit:     limit,
	}, nil
}

func (s *Service) ClearHistory(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return &ValidationError{
			Field:   "session_id",
			Message: "session_id is required",
		}
	}
	ctx, span := observability.StartSpan(
		ctx,
		"session.history.clear",
		attribute.String("session_id", sessionID),
	)
	defer span.End()

	if s.store == nil {
		observability.MarkError(span, "session history unavailable")
		return ErrSessionMemoryUnavailable
	}
	if err := s.store.ClearHistory(ctx, sessionID); err != nil {
		observability.MarkError(span, "session history clear failed")
		return fmt.Errorf("%w: clear history", ErrSessionMemoryUnavailable)
	}
	return nil
}

func validateHistoryQuery(query HistoryQuery) (string, int, error) {
	sessionID := strings.TrimSpace(query.SessionID)
	if sessionID == "" {
		return "", 0, &ValidationError{
			Field:   "session_id",
			Message: "session_id is required",
		}
	}
	if query.Limit < 0 {
		return "", 0, &ValidationError{
			Field:   "limit",
			Message: "limit must not be negative",
		}
	}
	limit := query.Limit
	if limit == 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}
	return sessionID, limit, nil
}
