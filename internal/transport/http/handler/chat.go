package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type ChatExecutor interface {
	Execute(context.Context, applicationchat.Command) (applicationchat.Result, error)
}

type ChatStreamer interface {
	Stream(
		context.Context,
		applicationchat.Command,
		applicationchat.StreamEmitter,
	) (applicationchat.Result, error)
}

type ChatHistoryExecutor interface {
	GetHistory(
		context.Context,
		applicationchat.HistoryQuery,
	) (applicationchat.HistoryResult, error)
	ClearHistory(context.Context, string) error
}

type Chat struct {
	executor ChatExecutor
	history  ChatHistoryExecutor
}

func NewChat(executor ChatExecutor) *Chat {
	history, _ := executor.(ChatHistoryExecutor)
	return &Chat{executor: executor, history: history}
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
		UserID:    request.UserID,
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

func (h *Chat) GetHistory(c *gin.Context) {
	requestID := c.GetString("request_id")
	if h.history == nil {
		writeError(
			c,
			http.StatusServiceUnavailable,
			"SESSION_MEMORY_UNAVAILABLE",
			"session history is unavailable",
			requestID,
		)
		return
	}
	limit := 0
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeError(
				c,
				http.StatusBadRequest,
				"INVALID_ARGUMENT",
				"limit must be an integer",
				requestID,
			)
			return
		}
		limit = parsed
	}
	result, err := h.history.GetHistory(
		c.Request.Context(),
		applicationchat.HistoryQuery{
			SessionID: c.Query("session_id"),
			Limit:     limit,
		},
	)
	if err != nil {
		writeChatHistoryError(c, err, requestID)
		return
	}
	c.JSON(http.StatusOK, mapChatHistoryResponse(result))
}

func (h *Chat) ClearHistory(c *gin.Context) {
	requestID := c.GetString("request_id")
	if h.history == nil {
		writeError(
			c,
			http.StatusServiceUnavailable,
			"SESSION_MEMORY_UNAVAILABLE",
			"session history is unavailable",
			requestID,
		)
		return
	}
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if err := h.history.ClearHistory(c.Request.Context(), sessionID); err != nil {
		writeChatHistoryError(c, err, requestID)
		return
	}
	c.JSON(http.StatusOK, dto.ClearChatHistoryResponse{
		SessionID: sessionID,
		Cleared:   true,
	})
}

func (h *Chat) Stream(c *gin.Context) {
	requestID := c.GetString("request_id")

	var request dto.ChatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "request body is invalid", requestID)
		return
	}

	streamer, ok := h.executor.(ChatStreamer)
	if !ok {
		writeError(c, http.StatusInternalServerError, "INTERNAL", "chat streaming is not available", requestID)
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, http.StatusInternalServerError, "INTERNAL", "streaming is not supported", requestID)
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	started := time.Now()
	command := applicationchat.Command{
		RequestID: requestID,
		SessionID: request.SessionID,
		UserID:    request.UserID,
		Message:   request.Message,
		TimeContext: common.TimeRange{
			From: request.TimeContext.From,
			To:   request.TimeContext.To,
		},
	}
	writeEvent := func(eventType string, data any) {
		_ = writeSSE(c.Writer, flusher, eventType, data)
	}
	emit := func(event applicationchat.StreamEvent) {
		if c.Request.Context().Err() != nil {
			return
		}
		writeEvent(event.Type, event.Data)
	}

	result, err := streamer.Stream(c.Request.Context(), command, emit)
	if err != nil {
		statusCode := "INTERNAL"
		message := "chat request could not be completed"
		var validationErr *applicationchat.ValidationError
		if errors.As(err, &validationErr) {
			statusCode = "INVALID_ARGUMENT"
			message = validationErr.Message
		}
		writeEvent("workflow_failed", map[string]any{
			"request_id": requestID,
			"error_code": statusCode,
			"message":    message,
			"latency_ms": time.Since(started).Milliseconds(),
		})
		return
	}

	response := mapChatResponse(result)
	writeEvent("final_answer", response)
	writeEvent("workflow_completed", map[string]any{
		"request_id": requestID,
		"trace_id":   result.TraceID,
		"latency_ms": time.Since(started).Milliseconds(),
	})
}

func writeChatHistoryError(c *gin.Context, err error, requestID string) {
	var validationErr *applicationchat.ValidationError
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
	if errors.Is(err, applicationchat.ErrSessionMemoryUnavailable) {
		writeError(
			c,
			http.StatusServiceUnavailable,
			"SESSION_MEMORY_UNAVAILABLE",
			"session history is unavailable",
			requestID,
		)
		return
	}
	writeError(
		c,
		http.StatusInternalServerError,
		"INTERNAL",
		"chat history request could not be completed",
		requestID,
	)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, encoded); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func mapChatHistoryResponse(
	result applicationchat.HistoryResult,
) dto.ChatHistoryResponse {
	summary := dto.ChatHistorySummary{
		Content:           result.Summary.Content,
		Version:           result.Summary.Version,
		Goal:              result.Summary.Goal,
		ConfirmedFacts:    nonNilStrings(result.Summary.ConfirmedFacts),
		OpenQuestions:     nonNilStrings(result.Summary.OpenQuestions),
		AttemptedActions:  nonNilStrings(result.Summary.AttemptedActions),
		ImportantEntities: nonNilStrings(result.Summary.ImportantEntities),
	}
	if !result.Summary.UpdatedAt.IsZero() {
		summary.UpdatedAt = result.Summary.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	messages := make([]dto.ChatHistoryMessage, 0, len(result.Messages))
	for _, message := range result.Messages {
		item := dto.ChatHistoryMessage{
			Role:      string(message.Role),
			Content:   message.Content,
			RequestID: message.RequestID,
			Metadata:  message.Metadata,
		}
		if !message.CreatedAt.IsZero() {
			item.CreatedAt = message.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		messages = append(messages, item)
	}
	return dto.ChatHistoryResponse{
		SessionID: result.SessionID,
		Summary:   summary,
		Messages:  messages,
		Limit:     result.Limit,
		Count:     len(messages),
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
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
