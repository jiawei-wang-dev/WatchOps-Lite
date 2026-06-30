package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type ChatExecutor interface {
	Execute(context.Context, applicationchat.Command) (applicationchat.Result, error)
}

type Chat struct {
	executor ChatExecutor
}

func NewChat(executor ChatExecutor) *Chat {
	return &Chat{executor: executor}
}

func (h *Chat) Handle(c *gin.Context) {
	requestID := c.GetString("request_id")

	var request dto.ChatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "request body is invalid", requestID)
		return
	}

	result, err := h.executor.Execute(c.Request.Context(), applicationchat.Command{
		RequestID: requestID,
		SessionID: request.SessionID,
		Message:   request.Message,
		TimeContext: common.TimeRange{
			From: request.TimeContext.From,
			To:   request.TimeContext.To,
		},
	})
	if err != nil {
		var validationErr *applicationchat.ValidationError
		if errors.As(err, &validationErr) {
			writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", validationErr.Message, requestID)
			return
		}

		writeError(c, http.StatusInternalServerError, "INTERNAL", "chat request could not be completed", requestID)
		return
	}

	c.JSON(http.StatusOK, mapChatResponse(result))
}

func mapChatResponse(result applicationchat.Result) dto.ChatResponse {
	response := dto.ChatResponse{
		RequestID: result.RequestID,
		SessionID: result.SessionID,
		Answer: dto.Answer{
			Conclusion:      make([]dto.ConclusionItem, 0, len(result.Agent.Conclusions)),
			Evidence:        make([]dto.EvidenceItemDTO, 0, len(result.Agent.Evidence)),
			Inferences:      make([]dto.InferenceItem, 0, len(result.Agent.Inferences)),
			Recommendations: make([]dto.RecommendationItem, 0, len(result.Agent.Recommendations)),
			Limitations:     make([]dto.LimitationItem, 0, len(result.Agent.Limitations)),
		},
		ToolRuns: make([]dto.ToolRunDTO, 0, len(result.Agent.ToolRuns)),
		TraceID:  result.TraceID,
		Metadata: result.Agent.Metadata,
	}

	for _, conclusion := range result.Agent.Conclusions {
		response.Answer.Conclusion = append(response.Answer.Conclusion, dto.ConclusionItem{
			Text:        conclusion.Text,
			EvidenceIDs: conclusion.EvidenceIDs,
		})
	}
	for _, evidence := range result.Agent.Evidence {
		response.Answer.Evidence = append(response.Answer.Evidence, mapEvidence(evidence))
	}
	for _, inference := range result.Agent.Inferences {
		response.Answer.Inferences = append(response.Answer.Inferences, dto.InferenceItem{
			Text:        inference.Text,
			EvidenceIDs: inference.EvidenceIDs,
		})
	}
	for _, recommendation := range result.Agent.Recommendations {
		response.Answer.Recommendations = append(response.Answer.Recommendations, dto.RecommendationItem{
			Text:        recommendation.Text,
			EvidenceIDs: recommendation.EvidenceIDs,
		})
	}
	for _, limitation := range result.Agent.Limitations {
		response.Answer.Limitations = append(response.Answer.Limitations, dto.LimitationItem{
			Code:    limitation.Code,
			Message: limitation.Message,
			Tool:    limitation.Tool,
		})
	}
	for _, toolRun := range result.Agent.ToolRuns {
		response.ToolRuns = append(response.ToolRuns, dto.ToolRunDTO{
			Tool:          toolRun.Tool,
			Success:       toolRun.Success,
			DurationMS:    toolRun.DurationMS,
			ErrorCode:     string(toolRun.ErrorCode),
			EvidenceCount: toolRun.EvidenceCount,
			WarningCount:  toolRun.WarningCount,
		})
	}

	return response
}

func mapEvidence(evidence common.EvidenceItem) dto.EvidenceItemDTO {
	result := dto.EvidenceItemDTO{
		ID:         evidence.ID,
		SourceType: evidence.SourceType,
		SourceName: evidence.SourceName,
		Content:    evidence.Content,
		ResourceID: evidence.ResourceID,
		Score:      evidence.Score,
		Confidence: evidence.Confidence,
		Metadata:   evidence.Metadata,
	}
	if evidence.TimeRange != nil {
		result.TimeRange = &dto.TimeContext{
			From: evidence.TimeRange.From,
			To:   evidence.TimeRange.To,
		}
	}
	return result
}

func writeError(c *gin.Context, status int, code string, message string, requestID string) {
	c.JSON(status, dto.ErrorResponse{
		Error: dto.APIError{
			Code:      code,
			Message:   message,
			RequestID: requestID,
			Details:   []any{},
		},
	})
}
