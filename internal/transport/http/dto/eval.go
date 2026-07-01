package dto

import "time"

type CreateEvalCaseRequest struct {
	FeedbackID        string         `json:"feedback_id"`
	CaseType          string         `json:"case_type" binding:"required"`
	InputMessage      string         `json:"input_message" binding:"required"`
	ExpectedBehavior  string         `json:"expected_behavior" binding:"required"`
	GoldAnswer        string         `json:"gold_answer"`
	ForbiddenPatterns []string       `json:"forbidden_patterns"`
	Metadata          map[string]any `json:"metadata"`
}

type CreateEvalCaseResponse struct {
	CaseID string `json:"case_id"`
	Status string `json:"status"`
}

type EvalCaseResponse struct {
	ID                string         `json:"id"`
	FeedbackID        string         `json:"feedback_id,omitempty"`
	CaseType          string         `json:"case_type"`
	InputMessage      string         `json:"input_message"`
	ExpectedBehavior  string         `json:"expected_behavior"`
	GoldAnswer        string         `json:"gold_answer"`
	ForbiddenPatterns []string       `json:"forbidden_patterns"`
	Metadata          map[string]any `json:"metadata"`
	CreatedAt         time.Time      `json:"created_at"`
}

type ListEvalCasesResponse struct {
	Cases []EvalCaseResponse `json:"cases"`
}

type CreateEvalRunRequest struct {
	CaseType string `json:"case_type"`
	Limit    int    `json:"limit"`
}

type EvalRunResponse struct {
	RunID       string    `json:"run_id"`
	CaseType    string    `json:"case_type"`
	Status      string    `json:"status"`
	Total       int       `json:"total"`
	Passed      int       `json:"passed"`
	Failed      int       `json:"failed"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

type EvalCaseResultResponse struct {
	ResultID       string    `json:"result_id"`
	RunID          string    `json:"run_id"`
	CaseID         string    `json:"case_id"`
	Passed         bool      `json:"passed"`
	FailureReasons []string  `json:"failure_reasons"`
	RequestID      string    `json:"request_id"`
	TraceID        string    `json:"trace_id"`
	DurationMS     int64     `json:"duration_ms"`
	CreatedAt      time.Time `json:"created_at"`
}

type ListEvalCaseResultsResponse struct {
	Results []EvalCaseResultResponse `json:"results"`
}
