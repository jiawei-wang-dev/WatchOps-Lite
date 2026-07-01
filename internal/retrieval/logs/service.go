package logs

import (
	"context"
	"fmt"
	"strings"
)

const maxSearchLimit = 100

type Service struct {
	store Store
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidArgument)
	}
	return &Service{store: store}, nil
}

func (s *Service) EnsureIndex(ctx context.Context) error {
	return s.store.EnsureIndex(ctx)
}

func (s *Service) Search(
	ctx context.Context,
	query SearchQuery,
) ([]Event, error) {
	query.Service = strings.TrimSpace(query.Service)
	query.Level = strings.ToLower(strings.TrimSpace(query.Level))
	query.Keywords = normalizeKeywords(query.Keywords)

	if query.Service == "" {
		return nil, fmt.Errorf("%w: service is required", ErrInvalidArgument)
	}
	if query.From.IsZero() || query.To.IsZero() || query.To.Before(query.From) {
		return nil, fmt.Errorf("%w: valid time range is required", ErrInvalidArgument)
	}
	switch query.Level {
	case "", "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("%w: unsupported level", ErrInvalidArgument)
	}
	if query.Limit < 1 || query.Limit > maxSearchLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidArgument, maxSearchLimit)
	}

	return s.store.Search(ctx, query)
}

func normalizeKeywords(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
