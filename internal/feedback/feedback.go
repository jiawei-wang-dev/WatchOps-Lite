package feedback

import (
	"errors"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid feedback")
	ErrNotFound        = errors.New("feedback not found")
	ErrUnavailable     = errors.New("feedback store unavailable")
)

type Rating string

const (
	RatingUp   Rating = "up"
	RatingDown Rating = "down"
)

type Feedback struct {
	ID              string           `json:"id"`
	RequestID       string           `json:"request_id"`
	SessionID       string           `json:"session_id"`
	Rating          Rating           `json:"rating"`
	ReasonTags      []string         `json:"reason_tags"`
	Comment         string           `json:"comment"`
	CorrectedAnswer string           `json:"corrected_answer"`
	AnswerSnapshot  map[string]any   `json:"answer_snapshot"`
	EvidenceIDs     []string         `json:"evidence_ids"`
	ToolRuns        []map[string]any `json:"tool_runs"`
	Metadata        map[string]any   `json:"metadata"`
	CreatedAt       time.Time        `json:"created_at"`
}

type CreateResult struct {
	FeedbackID string `json:"feedback_id"`
	Status     string `json:"status"`
}
