package eval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/feedback"
)

const (
	defaultListLimit = 50
	maxListLimit     = 100
)

type FeedbackReader interface {
	Get(context.Context, string) (feedback.Feedback, error)
}

type Service struct {
	store          Store
	feedbackReader FeedbackReader
	now            func() time.Time
	newID          func() (string, error)
}

func NewService(store Store, feedbackReader FeedbackReader) (*Service, error) {
	if store == nil || feedbackReader == nil {
		return nil, fmt.Errorf("%w: store and feedback reader are required", ErrInvalidArgument)
	}
	return &Service{
		store:          store,
		feedbackReader: feedbackReader,
		now:            func() time.Time { return time.Now().UTC() },
		newID:          generateCaseID,
	}, nil
}

func (s *Service) Create(ctx context.Context, value Case) (CreateResult, error) {
	value.FeedbackID = strings.TrimSpace(value.FeedbackID)
	value.CaseType = CaseType(strings.ToLower(strings.TrimSpace(string(value.CaseType))))
	value.InputMessage = strings.TrimSpace(value.InputMessage)
	value.ExpectedBehavior = strings.TrimSpace(value.ExpectedBehavior)
	value.GoldAnswer = strings.TrimSpace(value.GoldAnswer)
	if !validCaseType(value.CaseType) {
		return CreateResult{}, fmt.Errorf("%w: case_type must be good_case or bad_case", ErrInvalidArgument)
	}
	if value.InputMessage == "" || value.ExpectedBehavior == "" {
		return CreateResult{}, fmt.Errorf("%w: input_message and expected_behavior are required", ErrInvalidArgument)
	}
	for index, pattern := range value.ForbiddenPatterns {
		value.ForbiddenPatterns[index] = strings.TrimSpace(pattern)
		if value.ForbiddenPatterns[index] == "" {
			return CreateResult{}, fmt.Errorf("%w: forbidden_patterns must not contain empty values", ErrInvalidArgument)
		}
	}
	if value.FeedbackID != "" {
		source, err := s.feedbackReader.Get(ctx, value.FeedbackID)
		if err != nil {
			return CreateResult{}, err
		}
		if (source.Rating == feedback.RatingDown && value.CaseType != CaseTypeBad) ||
			(source.Rating == feedback.RatingUp && value.CaseType != CaseTypeGood) {
			return CreateResult{}, fmt.Errorf("%w: case_type does not match feedback rating", ErrInvalidArgument)
		}
	}

	id, err := s.newID()
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate eval case ID: %w", err)
	}
	value.ID = id
	value.CreatedAt = s.now()
	if value.ForbiddenPatterns == nil {
		value.ForbiddenPatterns = []string{}
	}
	if value.Metadata == nil {
		value.Metadata = map[string]any{}
	}
	if err := s.store.Create(ctx, value); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{CaseID: value.ID, Status: "created"}, nil
}

func (s *Service) List(ctx context.Context, query ListQuery) ([]Case, error) {
	query.CaseType = CaseType(strings.ToLower(strings.TrimSpace(string(query.CaseType))))
	if query.CaseType != "" && !validCaseType(query.CaseType) {
		return nil, fmt.Errorf("%w: case_type must be good_case or bad_case", ErrInvalidArgument)
	}
	if query.Limit == 0 {
		query.Limit = defaultListLimit
	}
	if query.Limit < 1 || query.Limit > maxListLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidArgument, maxListLimit)
	}
	return s.store.List(ctx, query)
}

func validCaseType(value CaseType) bool {
	return value == CaseTypeGood || value == CaseTypeBad
}

func generateCaseID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "eval_" + hex.EncodeToString(value[:]), nil
}
