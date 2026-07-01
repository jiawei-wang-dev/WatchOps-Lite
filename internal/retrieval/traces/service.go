package traces

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const maxTraceLimit = 100

type Service struct {
	store Store
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidArgument)
	}
	return &Service{store: store}, nil
}

func (s *Service) Search(ctx context.Context, query Query) ([]Span, error) {
	query.Service = strings.TrimSpace(query.Service)
	query.TraceID = strings.TrimSpace(query.TraceID)
	query.Operation = strings.TrimSpace(query.Operation)
	if query.Service == "" {
		return nil, fmt.Errorf("%w: service is required", ErrInvalidArgument)
	}
	if query.From.IsZero() || query.To.IsZero() || query.To.Before(query.From) {
		return nil, fmt.Errorf("%w: valid time range is required", ErrInvalidArgument)
	}
	if query.Limit < 1 || query.Limit > maxTraceLimit {
		return nil, fmt.Errorf(
			"%w: limit must be between 1 and %d",
			ErrInvalidArgument,
			maxTraceLimit,
		)
	}

	spans, err := s.store.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	filtered := make([]Span, 0, len(spans))
	for _, span := range spans {
		if query.TraceID == "" &&
			span.Service != "" &&
			!strings.EqualFold(span.Service, query.Service) {
			continue
		}
		if query.Operation != "" &&
			!strings.Contains(strings.ToLower(span.Operation), strings.ToLower(query.Operation)) {
			continue
		}
		filtered = append(filtered, span)
	}
	sort.SliceStable(filtered, func(left int, right int) bool {
		if filtered[left].Error != filtered[right].Error {
			return filtered[left].Error
		}
		return filtered[left].DurationMS > filtered[right].DurationMS
	})
	if len(filtered) > query.Limit {
		filtered = filtered[:query.Limit]
	}
	return filtered, nil
}
