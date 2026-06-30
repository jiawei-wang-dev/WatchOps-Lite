package eval

import "context"

type Store interface {
	Create(context.Context, Case) error
	List(context.Context, ListQuery) ([]Case, error)
}

type UnavailableStore struct{}

func (UnavailableStore) Create(context.Context, Case) error {
	return ErrUnavailable
}

func (UnavailableStore) List(context.Context, ListQuery) ([]Case, error) {
	return nil, ErrUnavailable
}
