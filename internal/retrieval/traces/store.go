package traces

import "context"

type Store interface {
	Search(context.Context, Query) ([]Span, error)
}

type UnavailableStore struct{}

func (UnavailableStore) Search(context.Context, Query) ([]Span, error) {
	return nil, ErrUnavailable
}
