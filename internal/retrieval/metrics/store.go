package metrics

import (
	"context"
	"time"
)

type Store interface {
	Query(context.Context, string, time.Time) ([]Sample, error)
}

type UnavailableStore struct{}

func (UnavailableStore) Query(context.Context, string, time.Time) ([]Sample, error) {
	return nil, ErrUnavailable
}
