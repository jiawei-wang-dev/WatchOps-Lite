package longterm

import "context"

type Store interface {
	Save(ctx context.Context, memory Memory) error
	Search(ctx context.Context, query SearchQuery) ([]Memory, error)
	Get(ctx context.Context, id string) (Memory, error)
}

type UnavailableStore struct{}

func (UnavailableStore) Save(context.Context, Memory) error {
	return ErrUnavailable
}

func (UnavailableStore) Search(context.Context, SearchQuery) ([]Memory, error) {
	return nil, ErrUnavailable
}

func (UnavailableStore) Get(context.Context, string) (Memory, error) {
	return Memory{}, ErrUnavailable
}
