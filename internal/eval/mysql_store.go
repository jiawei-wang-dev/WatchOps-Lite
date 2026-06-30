package eval

import (
	"context"
	"database/sql"
	"encoding/json"
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

func (s *MySQLStore) Create(ctx context.Context, value Case) error {
	forbiddenPatterns, err := encodeJSON(value.ForbiddenPatterns)
	if err != nil {
		return err
	}
	metadata, err := encodeJSON(value.Metadata)
	if err != nil {
		return err
	}
	feedbackID := sql.NullString{String: value.FeedbackID, Valid: value.FeedbackID != ""}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO eval_cases (
			id, feedback_id, case_type, input_message, expected_behavior,
			gold_answer, forbidden_patterns, metadata, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID,
		feedbackID,
		value.CaseType,
		value.InputMessage,
		value.ExpectedBehavior,
		value.GoldAnswer,
		forbiddenPatterns,
		metadata,
		value.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("%w: create eval case: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *MySQLStore) List(ctx context.Context, query ListQuery) ([]Case, error) {
	statement := `
		SELECT id, feedback_id, case_type, input_message, expected_behavior,
			gold_answer, forbidden_patterns, metadata, created_at
		FROM eval_cases`
	arguments := []any{}
	if query.CaseType != "" {
		statement += " WHERE case_type = ?"
		arguments = append(arguments, query.CaseType)
	}
	statement += " ORDER BY created_at DESC, id DESC LIMIT ?"
	arguments = append(arguments, query.Limit)

	rows, err := s.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("%w: list eval cases: %v", ErrUnavailable, err)
	}
	defer rows.Close()

	cases := make([]Case, 0)
	for rows.Next() {
		var value Case
		var feedbackID sql.NullString
		var caseType string
		var forbiddenPatterns, metadata []byte
		if err := rows.Scan(
			&value.ID,
			&feedbackID,
			&caseType,
			&value.InputMessage,
			&value.ExpectedBehavior,
			&value.GoldAnswer,
			&forbiddenPatterns,
			&metadata,
			&value.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("%w: scan eval case: %v", ErrUnavailable, err)
		}
		value.FeedbackID = feedbackID.String
		value.CaseType = CaseType(caseType)
		if err := decodeJSON(forbiddenPatterns, &value.ForbiddenPatterns); err != nil {
			return nil, err
		}
		if err := decodeJSON(metadata, &value.Metadata); err != nil {
			return nil, err
		}
		cases = append(cases, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate eval cases: %v", ErrUnavailable, err)
	}
	return cases, nil
}

func encodeJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode eval JSON: %w", err)
	}
	return encoded, nil
}

func decodeJSON(encoded []byte, target any) error {
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("%w: decode eval JSON: %v", ErrUnavailable, err)
	}
	return nil
}
