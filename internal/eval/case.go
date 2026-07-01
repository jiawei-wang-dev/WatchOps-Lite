package eval

import (
	"errors"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid eval case")
	ErrUnavailable     = errors.New("eval store unavailable")
	ErrNotFound        = errors.New("eval record not found")
)

type CaseType string

const (
	CaseTypeGood CaseType = "good_case"
	CaseTypeBad  CaseType = "bad_case"
)

type Case struct {
	ID                string         `json:"id"`
	FeedbackID        string         `json:"feedback_id,omitempty"`
	CaseType          CaseType       `json:"case_type"`
	InputMessage      string         `json:"input_message"`
	ExpectedBehavior  string         `json:"expected_behavior"`
	GoldAnswer        string         `json:"gold_answer"`
	ForbiddenPatterns []string       `json:"forbidden_patterns"`
	Metadata          map[string]any `json:"metadata"`
	CreatedAt         time.Time      `json:"created_at"`
}

type CreateResult struct {
	CaseID string `json:"case_id"`
	Status string `json:"status"`
}

type ListQuery struct {
	CaseType CaseType
	Limit    int
}
