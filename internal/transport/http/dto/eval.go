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
