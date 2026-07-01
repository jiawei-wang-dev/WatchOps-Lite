package traces

import (
	"errors"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid trace query")
	ErrUnavailable     = errors.New("trace backend unavailable")
)

type Span struct {
	TraceID        string
	SpanID         string
	ParentSpanID   string
	Service        string
	Operation      string
	StartTime      time.Time
	DurationMS     float64
	Tags           map[string]any
	ProcessService string
	Error          bool
}

type Query struct {
	Service   string
	TraceID   string
	Operation string
	From      time.Time
	To        time.Time
	Limit     int
}
