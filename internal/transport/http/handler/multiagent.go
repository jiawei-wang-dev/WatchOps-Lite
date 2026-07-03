package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/multiagent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type MultiAgentExecutor interface {
	Execute(context.Context, multiagent.Command) (multiagent.Result, error)
}

type MultiAgent struct {
	executor MultiAgentExecutor
}

func NewMultiAgent(executor MultiAgentExecutor) *MultiAgent {
	return &MultiAgent{executor: executor}
}

func (h *MultiAgent) Handle(c *gin.Context) {
	requestID := c.GetString("request_id")
	if h.executor == nil {
		writeError(
			c,
			http.StatusServiceUnavailable,
			"MULTI_AGENT_UNAVAILABLE",
			"multi-agent mode is unavailable",
			requestID,
		)
		return
	}
	var request dto.ChatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(
			c,
			http.StatusBadRequest,
			"INVALID_ARGUMENT",
			"request body is invalid",
			requestID,
		)
		return
	}
	result, err := h.executor.Execute(c.Request.Context(), multiagent.Command{
		RequestID: requestID,
		SessionID: request.SessionID,
		UserID:    request.UserID,
		Message:   request.Message,
		TimeContext: common.TimeRange{
			From: request.TimeContext.From,
			To:   request.TimeContext.To,
		},
		Metadata: request.Metadata,
	})
	if err != nil {
		var validationErr *multiagent.ValidationError
		if errors.As(err, &validationErr) {
			writeError(
				c,
				http.StatusBadRequest,
				"INVALID_ARGUMENT",
				validationErr.Message,
				requestID,
			)
			return
		}
		writeError(
			c,
			http.StatusInternalServerError,
			"INTERNAL",
			"multi-agent request could not be completed",
			requestID,
		)
		return
	}
	c.JSON(http.StatusOK, mapMultiAgentResponse(result))
}

func mapMultiAgentResponse(result multiagent.Result) dto.MultiAgentResponse {
	chatResponse := mapChatResponse(applicationchat.Result{
		RequestID: result.RequestID,
		SessionID: result.SessionID,
		Agent:     result.Output.FinalAnswer,
		TraceID:   result.TraceID,
	})
	evidence := make([]dto.EvidenceItemDTO, 0, len(result.Output.Evidence))
	for _, item := range result.Output.Evidence {
		evidence = append(evidence, mapEvidence(item))
	}
	steps := make([]dto.AgentStepDTO, 0, len(result.Output.Steps))
	for _, step := range result.Output.Steps {
		toolRuns := make([]dto.ToolRunDTO, 0, len(step.ToolRuns))
		for _, run := range step.ToolRuns {
			toolRuns = append(toolRuns, dto.ToolRunDTO{
				Tool:          run.Tool,
				Success:       run.Success,
				DurationMS:    run.DurationMS,
				ErrorCode:     string(run.ErrorCode),
				EvidenceCount: run.EvidenceCount,
				WarningCount:  run.WarningCount,
			})
		}
		limitations := make(
			[]dto.LimitationItem,
			0,
			len(step.Limitations),
		)
		for _, limitation := range step.Limitations {
			limitations = append(limitations, dto.LimitationItem{
				Code:    limitation.Code,
				Message: limitation.Message,
				Tool:    limitation.Tool,
			})
		}
		item := dto.AgentStepDTO{
			Role:        string(step.Role),
			Name:        step.Name,
			Status:      string(step.Status),
			Input:       step.Input,
			Output:      step.Output,
			EvidenceIDs: nonNilStrings(step.EvidenceIDs),
			ToolRuns:    toolRuns,
			Limitations: limitations,
			DurationMS:  step.DurationMS,
			Metadata:    step.Metadata,
		}
		if !step.StartedAt.IsZero() {
			item.StartedAt = step.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		if !step.CompletedAt.IsZero() {
			item.CompletedAt = step.CompletedAt.UTC().Format(time.RFC3339Nano)
		}
		steps = append(steps, item)
	}
	return dto.MultiAgentResponse{
		RequestID:  result.RequestID,
		SessionID:  result.SessionID,
		Mode:       "multi_agent",
		Answer:     chatResponse.Answer,
		AgentSteps: steps,
		Evidence:   evidence,
		ToolRuns:   chatResponse.ToolRuns,
		TraceID:    result.TraceID,
		Metadata:   result.Output.Metadata,
	}
}
