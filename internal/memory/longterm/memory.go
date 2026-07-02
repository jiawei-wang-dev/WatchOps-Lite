package longterm

import (
	"errors"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid long-term memory")
	ErrNotFound        = errors.New("long-term memory not found")
	ErrUnavailable     = errors.New("long-term memory store unavailable")
)

const (
	SourceFeedbackUp   = "feedback_up"
	SourceEvalGoodCase = "eval_good_case"
	SourceManual       = "manual"
)

type Memory struct {
	ID          string         `json:"id"`
	SourceType  string         `json:"source_type"`
	SourceID    string         `json:"source_id"`
	Service     string         `json:"service"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	EvidenceIDs []string       `json:"evidence_ids"`
	Tags        []string       `json:"tags"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type SearchQuery struct {
	Query   string
	Service string
	Tags    []string
	Limit   int
}

func ValidSourceType(value string) bool {
	switch value {
	case SourceFeedbackUp, SourceEvalGoodCase, SourceManual:
		return true
	default:
		return false
	}
}
