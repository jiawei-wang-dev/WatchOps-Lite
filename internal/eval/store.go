package eval

import "context"

type Store interface {
	Create(context.Context, Case) error
	List(context.Context, ListQuery) ([]Case, error)
}

type RunStore interface {
	CreateRun(context.Context, Run) error
	SaveCaseResult(context.Context, CaseResult) error
	CompleteRun(context.Context, Run) error
	GetRun(context.Context, string) (Run, error)
	ListCaseResults(context.Context, string) ([]CaseResult, error)
}

type UnavailableStore struct{}

func (UnavailableStore) Create(context.Context, Case) error {
	return ErrUnavailable
}

func (UnavailableStore) List(context.Context, ListQuery) ([]Case, error) {
	return nil, ErrUnavailable
}

func (UnavailableStore) CreateRun(context.Context, Run) error {
	return ErrUnavailable
}

func (UnavailableStore) SaveCaseResult(context.Context, CaseResult) error {
	return ErrUnavailable
}

func (UnavailableStore) CompleteRun(context.Context, Run) error {
	return ErrUnavailable
}

func (UnavailableStore) GetRun(context.Context, string) (Run, error) {
	return Run{}, ErrUnavailable
}

func (UnavailableStore) ListCaseResults(context.Context, string) ([]CaseResult, error) {
	return nil, ErrUnavailable
}
