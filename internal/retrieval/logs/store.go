package logs

import "context"

type Store interface {
	EnsureIndex(context.Context) error
	Search(context.Context, SearchQuery) ([]Event, error)
}

type UnavailableStore struct{}

func (UnavailableStore) EnsureIndex(context.Context) error {
	return ErrUnavailable
}

func (UnavailableStore) Search(context.Context, SearchQuery) ([]Event, error) {
	return nil, ErrUnavailable
}
