package profile

import "context"

type Store interface {
	Upsert(ctx context.Context, value Profile) error
	Get(ctx context.Context, userID string) (Profile, error)
}

type Loader interface {
	LoadProfile(ctx context.Context, userID string) (Profile, error)
}
