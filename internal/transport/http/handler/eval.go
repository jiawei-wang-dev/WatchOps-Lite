package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/feedback"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type EvalExecutor interface {
	Create(context.Context, eval.Case) (eval.CreateResult, error)
	List(context.Context, eval.ListQuery) ([]eval.Case, error)
}

type Eval struct {
	executor EvalExecutor
}

func NewEval(executor EvalExecutor) *Eval {
	return &Eval{executor: executor}
}

func (h *Eval) Create(c *gin.Context) {
	var request dto.CreateEvalCaseRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "request body is invalid", c.GetString("request_id"))
		return
	}
	result, err := h.executor.Create(c.Request.Context(), eval.Case{
		FeedbackID:        request.FeedbackID,
		CaseType:          eval.CaseType(request.CaseType),
		InputMessage:      request.InputMessage,
		ExpectedBehavior:  request.ExpectedBehavior,
		GoldAnswer:        request.GoldAnswer,
		ForbiddenPatterns: request.ForbiddenPatterns,
		Metadata:          request.Metadata,
	})
	if err != nil {
		writeEvalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.CreateEvalCaseResponse{
		CaseID: result.CaseID,
		Status: result.Status,
	})
}

func (h *Eval) List(c *gin.Context) {
	limit := 0
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "limit must be an integer", c.GetString("request_id"))
			return
		}
		limit = parsed
	}
	results, err := h.executor.List(c.Request.Context(), eval.ListQuery{
		CaseType: eval.CaseType(c.Query("case_type")),
		Limit:    limit,
	})
	if err != nil {
		writeEvalError(c, err)
		return
	}
	response := dto.ListEvalCasesResponse{Cases: make([]dto.EvalCaseResponse, 0, len(results))}
	for _, result := range results {
		response.Cases = append(response.Cases, dto.EvalCaseResponse{
			ID:                result.ID,
			FeedbackID:        result.FeedbackID,
			CaseType:          string(result.CaseType),
			InputMessage:      result.InputMessage,
			ExpectedBehavior:  result.ExpectedBehavior,
			GoldAnswer:        result.GoldAnswer,
			ForbiddenPatterns: result.ForbiddenPatterns,
			Metadata:          result.Metadata,
			CreatedAt:         result.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, response)
}

func writeEvalError(c *gin.Context, err error) {
	requestID := c.GetString("request_id")
	switch {
	case errors.Is(err, eval.ErrInvalidArgument), errors.Is(err, feedback.ErrInvalidArgument):
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), requestID)
	case errors.Is(err, feedback.ErrNotFound):
		writeError(c, http.StatusNotFound, "NOT_FOUND", "feedback was not found", requestID)
	case errors.Is(err, eval.ErrUnavailable), errors.Is(err, feedback.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "eval storage is unavailable", requestID)
	default:
		writeError(c, http.StatusInternalServerError, "INTERNAL", "eval request could not be completed", requestID)
	}
}
