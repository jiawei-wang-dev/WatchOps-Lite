package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/feedback"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type FeedbackExecutor interface {
	Create(context.Context, feedback.Feedback) (feedback.CreateResult, error)
	Get(context.Context, string) (feedback.Feedback, error)
}

type Feedback struct {
	executor FeedbackExecutor
}

func NewFeedback(executor FeedbackExecutor) *Feedback {
	return &Feedback{executor: executor}
}

func (h *Feedback) Create(c *gin.Context) {
	var request dto.CreateFeedbackRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "request body is invalid", c.GetString("request_id"))
		return
	}
	result, err := h.executor.Create(c.Request.Context(), feedback.Feedback{
		RequestID:       request.RequestID,
		SessionID:       request.SessionID,
		Rating:          feedback.Rating(request.Rating),
		ReasonTags:      request.ReasonTags,
		Comment:         request.Comment,
		CorrectedAnswer: request.CorrectedAnswer,
		AnswerSnapshot:  request.AnswerSnapshot,
		EvidenceIDs:     request.EvidenceIDs,
		ToolRuns:        request.ToolRuns,
		Metadata:        request.Metadata,
	})
	if err != nil {
		writeFeedbackError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.CreateFeedbackResponse{
		FeedbackID: result.FeedbackID,
		Status:     result.Status,
	})
}

func (h *Feedback) Get(c *gin.Context) {
	result, err := h.executor.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeFeedbackError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FeedbackResponse{
		ID:              result.ID,
		RequestID:       result.RequestID,
		SessionID:       result.SessionID,
		Rating:          string(result.Rating),
		ReasonTags:      result.ReasonTags,
		Comment:         result.Comment,
		CorrectedAnswer: result.CorrectedAnswer,
		AnswerSnapshot:  result.AnswerSnapshot,
		EvidenceIDs:     result.EvidenceIDs,
		ToolRuns:        result.ToolRuns,
		Metadata:        result.Metadata,
		CreatedAt:       result.CreatedAt,
	})
}

func writeFeedbackError(c *gin.Context, err error) {
	requestID := c.GetString("request_id")
	switch {
	case errors.Is(err, feedback.ErrInvalidArgument):
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), requestID)
	case errors.Is(err, feedback.ErrNotFound):
		writeError(c, http.StatusNotFound, "NOT_FOUND", "feedback was not found", requestID)
	case errors.Is(err, feedback.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "feedback storage is unavailable", requestID)
	default:
		writeError(c, http.StatusInternalServerError, "INTERNAL", "feedback request could not be completed", requestID)
	}
}
