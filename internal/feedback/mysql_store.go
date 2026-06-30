package feedback

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func (s *MySQLStore) Create(ctx context.Context, value Feedback) error {
	reasonTags, err := encodeJSON(value.ReasonTags)
	if err != nil {
		return err
	}
	answerSnapshot, err := encodeJSON(value.AnswerSnapshot)
	if err != nil {
		return err
	}
	evidenceIDs, err := encodeJSON(value.EvidenceIDs)
	if err != nil {
		return err
	}
	toolRuns, err := encodeJSON(value.ToolRuns)
	if err != nil {
		return err
	}
	metadata, err := encodeJSON(value.Metadata)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO feedback (
			id, request_id, session_id, rating, reason_tags, `+"`comment`"+`,
			corrected_answer, answer_snapshot, evidence_ids, tool_runs,
			metadata, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID,
		value.RequestID,
		value.SessionID,
		value.Rating,
		reasonTags,
		value.Comment,
		value.CorrectedAnswer,
		answerSnapshot,
		evidenceIDs,
		toolRuns,
		metadata,
		value.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("%w: create feedback: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *MySQLStore) Get(ctx context.Context, id string) (Feedback, error) {
	var value Feedback
	var rating string
	var reasonTags, answerSnapshot, evidenceIDs, toolRuns, metadata []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT id, request_id, session_id, rating, reason_tags, `+"`comment`"+`,
			corrected_answer, answer_snapshot, evidence_ids, tool_runs,
			metadata, created_at
		FROM feedback
		WHERE id = ?`,
		id,
	).Scan(
		&value.ID,
		&value.RequestID,
		&value.SessionID,
		&rating,
		&reasonTags,
		&value.Comment,
		&value.CorrectedAnswer,
		&answerSnapshot,
		&evidenceIDs,
		&toolRuns,
		&metadata,
		&value.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Feedback{}, ErrNotFound
	}
	if err != nil {
		return Feedback{}, fmt.Errorf("%w: get feedback: %v", ErrUnavailable, err)
	}
	value.Rating = Rating(rating)
	if err := decodeJSON(reasonTags, &value.ReasonTags); err != nil {
		return Feedback{}, err
	}
	if err := decodeJSON(answerSnapshot, &value.AnswerSnapshot); err != nil {
		return Feedback{}, err
	}
	if err := decodeJSON(evidenceIDs, &value.EvidenceIDs); err != nil {
		return Feedback{}, err
	}
	if err := decodeJSON(toolRuns, &value.ToolRuns); err != nil {
		return Feedback{}, err
	}
	if err := decodeJSON(metadata, &value.Metadata); err != nil {
		return Feedback{}, err
	}
	return value, nil
}

func encodeJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode feedback JSON: %w", err)
	}
	return encoded, nil
}

func decodeJSON(encoded []byte, target any) error {
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("%w: decode feedback JSON: %v", ErrUnavailable, err)
	}
	return nil
}
