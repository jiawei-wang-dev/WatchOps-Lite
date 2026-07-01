package eval

import (
	"context"
	"time"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
)

type RunRequest struct {
	CaseType CaseType
	Limit    int
}

type Run struct {
	ID          string    `json:"run_id"`
	CaseType    CaseType  `json:"case_type"`
	Status      RunStatus `json:"status"`
	Total       int       `json:"total"`
	Passed      int       `json:"passed"`
	Failed      int       `json:"failed"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at"`
}

type CaseResult struct {
	ID             string    `json:"result_id"`
	RunID          string    `json:"run_id"`
	CaseID         string    `json:"case_id"`
	Passed         bool      `json:"passed"`
	FailureReasons []string  `json:"failure_reasons"`
	RequestID      string    `json:"request_id"`
	TraceID        string    `json:"trace_id"`
	DurationMS     int64     `json:"duration_ms"`
	CreatedAt      time.Time `json:"created_at"`
}

type EvalInput struct {
	RequestID string
	SessionID string
	Message   string
}

type EvalOutput struct {
	RequestID           string
	TraceID             string
	Text                string
	EvidenceCount       int
	ToolRunCount        int
	ToolErrorCount      int
	LimitationCount     int
	ConclusionCount     int
	RecommendationCount int
}

type CaseExecutor interface {
	ExecuteEvalCase(context.Context, EvalInput) (EvalOutput, error)
}

type CaseExecutorFunc func(context.Context, EvalInput) (EvalOutput, error)

func (function CaseExecutorFunc) ExecuteEvalCase(
	ctx context.Context,
	input EvalInput,
) (EvalOutput, error) {
	return function(ctx, input)
}
