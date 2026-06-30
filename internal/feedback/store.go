package feedback

import "context"

type Store interface {
	Create(context.Context, Feedback) error
	Get(context.Context, string) (Feedback, error)
}

type UnavailableStore struct{}

func (UnavailableStore) Create(context.Context, Feedback) error {
	return ErrUnavailable
}

func (UnavailableStore) Get(context.Context, string) (Feedback, error) {
	return Feedback{}, ErrUnavailable
}
