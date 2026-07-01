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

func (s *MySQLStore) CreateRun(ctx context.Context, run Run) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO eval_runs (
			id, case_type, status, total, passed, failed, created_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		run.ID,
		run.CaseType,
		run.Status,
		run.Total,
		run.Passed,
		run.Failed,
		run.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("%w: create eval run: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *MySQLStore) SaveCaseResult(ctx context.Context, result CaseResult) error {
	failureReasons, err := encodeJSON(result.FailureReasons)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO eval_case_results (
			id, run_id, case_id, passed, failure_reasons,
			request_id, trace_id, duration_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		result.ID,
		result.RunID,
		result.CaseID,
		result.Passed,
		failureReasons,
		result.RequestID,
		result.TraceID,
		result.DurationMS,
		result.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("%w: save eval case result: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *MySQLStore) CompleteRun(ctx context.Context, run Run) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE eval_runs
		SET status = ?, total = ?, passed = ?, failed = ?, completed_at = ?
		WHERE id = ?`,
		run.Status,
		run.Total,
		run.Passed,
		run.Failed,
		run.CompletedAt,
		run.ID,
	)
	if err != nil {
		return fmt.Errorf("%w: complete eval run: %v", ErrUnavailable, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: inspect eval run update: %v", ErrUnavailable, err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MySQLStore) GetRun(ctx context.Context, runID string) (Run, error) {
	var run Run
	var caseType, status string
	var completedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT id, case_type, status, total, passed, failed, created_at, completed_at
		FROM eval_runs
		WHERE id = ?`,
		runID,
	).Scan(
		&run.ID,
		&caseType,
		&status,
		&run.Total,
		&run.Passed,
		&run.Failed,
		&run.CreatedAt,
		&completedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("%w: get eval run: %v", ErrUnavailable, err)
	}
	run.CaseType = CaseType(caseType)
	run.Status = RunStatus(status)
	if completedAt.Valid {
		run.CompletedAt = completedAt.Time
	}
	return run, nil
}

func (s *MySQLStore) ListCaseResults(
	ctx context.Context,
	runID string,
) ([]CaseResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, case_id, passed, failure_reasons,
			request_id, trace_id, duration_ms, created_at
		FROM eval_case_results
		WHERE run_id = ?
		ORDER BY created_at ASC, id ASC`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: list eval case results: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	results := make([]CaseResult, 0)
	for rows.Next() {
		var result CaseResult
		var failureReasons []byte
		if err := rows.Scan(
			&result.ID,
			&result.RunID,
			&result.CaseID,
			&result.Passed,
			&failureReasons,
			&result.RequestID,
			&result.TraceID,
			&result.DurationMS,
			&result.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("%w: scan eval case result: %v", ErrUnavailable, err)
		}
		if err := decodeJSON(failureReasons, &result.FailureReasons); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate eval case results: %v", ErrUnavailable, err)
	}
	return results, nil
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
