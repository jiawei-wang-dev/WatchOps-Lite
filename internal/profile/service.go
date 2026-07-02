package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type Manager struct {
	store Store
}

func NewManager(store Store) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidArgument)
	}
	return &Manager{store: store}, nil
}

func (m *Manager) LoadProfile(ctx context.Context, userID string) (Profile, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Profile{}, fmt.Errorf("%w: user_id is required", ErrInvalidArgument)
	}
	ctx, span := observability.StartSpan(
		ctx,
		"profile.load",
		attribute.String("user_id", userID),
	)
	defer span.End()

	value, err := m.store.Get(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			observability.MarkError(span, "user profile load failed")
		}
		return Profile{}, err
	}
	span.SetAttributes(attribute.Int("service_count", len(value.Services)))
	return value, nil
}

var _ Loader = (*Manager)(nil)
