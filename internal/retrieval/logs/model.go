package logs

import (
	"errors"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid logs request")
	ErrUnavailable     = errors.New("logs backend unavailable")
)

type Event struct {
	ID         string         `json:"id"`
	Timestamp  time.Time      `json:"timestamp"`
	Service    string         `json:"service"`
	Level      string         `json:"level"`
	Message    string         `json:"message"`
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	Attributes map[string]any `json:"attributes"`
	CreatedAt  time.Time      `json:"created_at"`
}

type SearchQuery struct {
	Service  string
	From     time.Time
	To       time.Time
	Keywords []string
	Level    string
	Limit    int
}
