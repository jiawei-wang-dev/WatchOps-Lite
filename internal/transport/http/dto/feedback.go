package dto

import "time"

type CreateFeedbackRequest struct {
	RequestID       string           `json:"request_id" binding:"required"`
	SessionID       string           `json:"session_id" binding:"required"`
	Rating          string           `json:"rating" binding:"required"`
	ReasonTags      []string         `json:"reason_tags"`
	Comment         string           `json:"comment"`
	CorrectedAnswer string           `json:"corrected_answer"`
	AnswerSnapshot  map[string]any   `json:"answer_snapshot"`
	EvidenceIDs     []string         `json:"evidence_ids"`
	ToolRuns        []map[string]any `json:"tool_runs"`
	Metadata        map[string]any   `json:"metadata"`
}

type CreateFeedbackResponse struct {
	FeedbackID string `json:"feedback_id"`
	Status     string `json:"status"`
}

type FeedbackResponse struct {
	ID              string           `json:"id"`
	RequestID       string           `json:"request_id"`
	SessionID       string           `json:"session_id"`
	Rating          string           `json:"rating"`
	ReasonTags      []string         `json:"reason_tags"`
	Comment         string           `json:"comment"`
	CorrectedAnswer string           `json:"corrected_answer"`
	AnswerSnapshot  map[string]any   `json:"answer_snapshot"`
	EvidenceIDs     []string         `json:"evidence_ids"`
	ToolRuns        []map[string]any `json:"tool_runs"`
	Metadata        map[string]any   `json:"metadata"`
	CreatedAt       time.Time        `json:"created_at"`
}
