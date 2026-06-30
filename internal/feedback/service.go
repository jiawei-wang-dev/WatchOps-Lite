package feedback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
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
	value.RequestID = strings.TrimSpace(value.RequestID)
	value.SessionID = strings.TrimSpace(value.SessionID)
	value.Rating = Rating(strings.ToLower(strings.TrimSpace(string(value.Rating))))
	value.Comment = strings.TrimSpace(value.Comment)
	value.CorrectedAnswer = strings.TrimSpace(value.CorrectedAnswer)
	if value.RequestID == "" || value.SessionID == "" {
		return CreateResult{}, fmt.Errorf("%w: request_id and session_id are required", ErrInvalidArgument)
	}
	if value.Rating != RatingUp && value.Rating != RatingDown {
		return CreateResult{}, fmt.Errorf("%w: rating must be up or down", ErrInvalidArgument)
	}
	for index, tag := range value.ReasonTags {
		value.ReasonTags[index] = strings.TrimSpace(tag)
		if value.ReasonTags[index] == "" {
			return CreateResult{}, fmt.Errorf("%w: reason_tags must not contain empty values", ErrInvalidArgument)
		}
	}

	id, err := s.newID()
	if err != nil {
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
		return CreateResult{}, err
	}
	return CreateResult{FeedbackID: value.ID, Status: "created"}, nil
}

func (s *Service) Get(ctx context.Context, id string) (Feedback, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Feedback{}, fmt.Errorf("%w: feedback ID is required", ErrInvalidArgument)
	}
	return s.store.Get(ctx, id)
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
