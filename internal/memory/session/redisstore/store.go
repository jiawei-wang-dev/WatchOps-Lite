package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/redis/go-redis/v9"
)

const (
	summaryDataField    = "data"
	summaryVersionField = "version"
)

type Store struct {
	client           *redis.Client
	recentWindowSize int
	ttl              time.Duration
}

func New(client *redis.Client, recentWindowSize int, ttl time.Duration) (*Store, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	if recentWindowSize <= 0 {
		return nil, errors.New("recent window size must be greater than zero")
	}
	if ttl <= 0 {
		return nil, errors.New("session TTL must be greater than zero")
	}

	return &Store{
		client:           client,
		recentWindowSize: recentWindowSize,
		ttl:              ttl,
	}, nil
}

func (s *Store) AppendMessage(ctx context.Context, sessionID string, message session.Message) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if err := message.Validate(); err != nil {
		return fmt.Errorf("validate session message: %w", err)
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}

	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode session message: %w", err)
	}

	key := recentKey(sessionID)
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.RPush(ctx, key, encoded)
		pipe.LTrim(ctx, key, int64(-s.recentWindowSize), -1)
		pipe.Expire(ctx, key, s.ttl)
		return nil
	})
	if err != nil {
		return fmt.Errorf("append recent session message: %w", err)
	}
	return nil
}

func (s *Store) GetRecentMessages(
	ctx context.Context,
	sessionID string,
	limit int,
) ([]session.Message, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > s.recentWindowSize {
		limit = s.recentWindowSize
	}

	values, err := s.client.LRange(ctx, recentKey(sessionID), int64(-limit), -1).Result()
	if err != nil {
		return nil, fmt.Errorf("read recent session messages: %w", err)
	}

	messages := make([]session.Message, 0, len(values))
	for _, value := range values {
		var message session.Message
		if err := json.Unmarshal([]byte(value), &message); err != nil {
			return nil, fmt.Errorf("decode recent session message: %w", err)
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (s *Store) GetSummary(ctx context.Context, sessionID string) (session.Summary, error) {
	if err := validateSessionID(sessionID); err != nil {
		return session.Summary{}, err
	}

	value, err := s.client.HGet(ctx, summaryKey(sessionID), summaryDataField).Result()
	if errors.Is(err, redis.Nil) {
		return session.EmptySummary(), nil
	}
	if err != nil {
		return session.Summary{}, fmt.Errorf("read session summary: %w", err)
	}

	summary := session.EmptySummary()
	if err := json.Unmarshal([]byte(value), &summary); err != nil {
		return session.Summary{}, fmt.Errorf("decode session summary: %w", err)
	}
	return summary, nil
}

func (s *Store) UpdateSummary(
	ctx context.Context,
	sessionID string,
	summary session.Summary,
	expectedVersion int64,
) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if expectedVersion < 0 {
		return errors.New("expected summary version must not be negative")
	}

	key := summaryKey(sessionID)
	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		currentVersion, err := readVersion(ctx, tx, key)
		if err != nil {
			return err
		}
		if currentVersion != expectedVersion {
			return session.ErrVersionConflict
		}

		summary.Version = expectedVersion + 1
		summary.UpdatedAt = time.Now().UTC()
		encoded, err := json.Marshal(summary)
		if err != nil {
			return fmt.Errorf("encode session summary: %w", err)
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(
				ctx,
				key,
				summaryVersionField,
				summary.Version,
				summaryDataField,
				encoded,
			)
			pipe.Expire(ctx, key, s.ttl)
			return nil
		})
		return err
	}, key)
	if errors.Is(err, redis.TxFailedErr) {
		return session.ErrVersionConflict
	}
	if err != nil {
		return fmt.Errorf("update session summary: %w", err)
	}
	return nil
}

func (s *Store) LoadContext(ctx context.Context, sessionID string) (session.ContextSnapshot, error) {
	summary, err := s.GetSummary(ctx, sessionID)
	if err != nil {
		return session.ContextSnapshot{}, err
	}
	messages, err := s.GetRecentMessages(ctx, sessionID, s.recentWindowSize)
	if err != nil {
		return session.ContextSnapshot{}, err
	}
	return session.ContextSnapshot{
		Summary:        summary,
		RecentMessages: messages,
	}, nil
}

func (s *Store) ClearHistory(ctx context.Context, sessionID string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if err := s.client.Del(
		ctx,
		recentKey(sessionID),
		summaryKey(sessionID),
	).Err(); err != nil {
		return fmt.Errorf("clear session history: %w", err)
	}
	return nil
}

func readVersion(ctx context.Context, tx *redis.Tx, key string) (int64, error) {
	value, err := tx.HGet(ctx, key, summaryVersionField).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read session summary version: %w", err)
	}

	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errors.New("stored session summary version is invalid")
	}
	return version, nil
}

func validateSessionID(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("session ID is required")
	}
	return nil
}

func recentKey(sessionID string) string {
	return "session:" + sessionID + ":recent"
}

func summaryKey(sessionID string) string {
	return "session:" + sessionID + ":summary"
}

var _ session.Store = (*Store)(nil)
