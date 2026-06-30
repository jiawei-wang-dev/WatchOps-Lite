package feedback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type Service struct {
	store Store
	now   func() time.Time
	newID func() (string, error)
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidArgument)
	}
	return &Service{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		newID: func() (string, error) { return generateID("fb_") },
	}, nil
}

func (s *Service) Create(ctx context.Context, value Feedback) (CreateResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"feedback.create",
		attribute.String("request_id", value.RequestID),
		attribute.String("session_id", value.SessionID),
		attribute.String("rating", string(value.Rating)),
	)
	defer span.End()

	value.RequestID = strings.TrimSpace(value.RequestID)
	value.SessionID = strings.TrimSpace(value.SessionID)
	value.Rating = Rating(strings.ToLower(strings.TrimSpace(string(value.Rating))))
	value.Comment = strings.TrimSpace(value.Comment)
	value.CorrectedAnswer = strings.TrimSpace(value.CorrectedAnswer)
	if value.RequestID == "" || value.SessionID == "" {
		observability.MarkError(span, "feedback validation failed")
		return CreateResult{}, fmt.Errorf("%w: request_id and session_id are required", ErrInvalidArgument)
	}
	if value.Rating != RatingUp && value.Rating != RatingDown {
		observability.MarkError(span, "feedback validation failed")
		return CreateResult{}, fmt.Errorf("%w: rating must be up or down", ErrInvalidArgument)
	}
	for index, tag := range value.ReasonTags {
		value.ReasonTags[index] = strings.TrimSpace(tag)
		if value.ReasonTags[index] == "" {
			observability.MarkError(span, "feedback validation failed")
			return CreateResult{}, fmt.Errorf("%w: reason_tags must not contain empty values", ErrInvalidArgument)
		}
	}

	id, err := s.newID()
	if err != nil {
		observability.MarkError(span, "feedback ID generation failed")
		return CreateResult{}, fmt.Errorf("generate feedback ID: %w", err)
	}
	value.ID = id
	value.CreatedAt = s.now()
	value.ReasonTags = emptySlice(value.ReasonTags)
	value.EvidenceIDs = emptySlice(value.EvidenceIDs)
	if value.ToolRuns == nil {
		value.ToolRuns = []map[string]any{}
	}
	if value.AnswerSnapshot == nil {
		value.AnswerSnapshot = map[string]any{}
	}
	if value.Metadata == nil {
		value.Metadata = map[string]any{}
	}
	if err := s.store.Create(ctx, value); err != nil {
		observability.MarkError(span, "feedback persistence failed")
		return CreateResult{}, err
	}
	span.SetAttributes(
		attribute.String("feedback_id", value.ID),
		attribute.String("rating", string(value.Rating)),
	)
	return CreateResult{FeedbackID: value.ID, Status: "created"}, nil
}

func (s *Service) Get(ctx context.Context, id string) (Feedback, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"feedback.get",
		attribute.String("feedback_id", id),
	)
	defer span.End()

	id = strings.TrimSpace(id)
	if id == "" {
		observability.MarkError(span, "feedback validation failed")
		return Feedback{}, fmt.Errorf("%w: feedback ID is required", ErrInvalidArgument)
	}
	result, err := s.store.Get(ctx, id)
	if err != nil {
		observability.MarkError(span, "feedback lookup failed")
		return Feedback{}, err
	}
	span.SetAttributes(attribute.String("rating", string(result.Rating)))
	return result, nil
}

func emptySlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func generateID(prefix string) (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}
