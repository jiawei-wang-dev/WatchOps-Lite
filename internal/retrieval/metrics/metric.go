package metrics

import (
	"errors"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid metrics query")
	ErrUnavailable     = errors.New("metrics backend unavailable")
)

type Sample struct {
	Name      string
	Value     float64
	Timestamp time.Time
	Service   string
	Labels    map[string]string
	Query     string
}

type QueryRequest struct {
	Service    string
	MetricName string
	Symptom    string
	At         time.Time
}
