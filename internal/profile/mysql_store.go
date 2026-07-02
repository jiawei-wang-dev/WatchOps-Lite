package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(db *sql.DB) (*MySQLStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: database is required", ErrInvalidArgument)
	}
	return &MySQLStore{db: db}, nil
}

func (s *MySQLStore) Upsert(ctx context.Context, value Profile) error {
	if err := value.Validate(); err != nil {
		return err
	}
	services, err := json.Marshal(value.Services)
	if err != nil {
		return fmt.Errorf("%w: encode services", ErrInvalidArgument)
	}
	preferences, err := json.Marshal(value.Preferences)
	if err != nil {
		return fmt.Errorf("%w: encode preferences", ErrInvalidArgument)
	}
	metadata, err := json.Marshal(value.Metadata)
	if err != nil {
		return fmt.Errorf("%w: encode metadata", ErrInvalidArgument)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO user_profiles (
			user_id, display_name, default_service, services, timezone,
			preferences, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			display_name = VALUES(display_name),
			default_service = VALUES(default_service),
			services = VALUES(services),
			timezone = VALUES(timezone),
			preferences = VALUES(preferences),
			metadata = VALUES(metadata),
			updated_at = VALUES(updated_at)`,
		value.UserID,
		value.DisplayName,
		value.DefaultService,
		services,
		value.Timezone,
		preferences,
		metadata,
		value.CreatedAt,
		value.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("%w: upsert profile", ErrUnavailable)
	}
	return nil
}

func (s *MySQLStore) Get(ctx context.Context, userID string) (Profile, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Profile{}, fmt.Errorf("%w: user_id is required", ErrInvalidArgument)
	}
	var value Profile
	var services, preferences, metadata []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT user_id, display_name, default_service, services, timezone,
			preferences, metadata, created_at, updated_at
		FROM user_profiles
		WHERE user_id = ?`,
		userID,
	).Scan(
		&value.UserID,
		&value.DisplayName,
		&value.DefaultService,
		&services,
		&value.Timezone,
		&preferences,
		&metadata,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("%w: get profile", ErrUnavailable)
	}
	if err := json.Unmarshal(services, &value.Services); err != nil {
		return Profile{}, fmt.Errorf("%w: decode services", ErrUnavailable)
	}
	if err := json.Unmarshal(preferences, &value.Preferences); err != nil {
		return Profile{}, fmt.Errorf("%w: decode preferences", ErrUnavailable)
	}
	if err := json.Unmarshal(metadata, &value.Metadata); err != nil {
		return Profile{}, fmt.Errorf("%w: decode metadata", ErrUnavailable)
	}
	return value, nil
}

var _ Store = (*MySQLStore)(nil)
