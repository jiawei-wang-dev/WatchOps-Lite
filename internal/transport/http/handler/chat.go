package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/multiagent"
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
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Header().Del("Content-Length")
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
	streamWriter := newSerialSSEWriter(
		c.Request.Context(),
		c.Writer,
		flusher,
	)
	writeEvent := streamWriter.Write
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
	metadata := cloneResponseMetadata(result.Agent.Metadata)
	ensureFinalDiagnosisMetadata(metadata, result.Agent, "single_agent")
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
		Metadata: metadata,
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
		response.ToolRuns = append(response.ToolRuns, mapToolRun(toolRun))
	}

	return response
}

func mapToolRun(toolRun agenteino.ToolRun) dto.ToolRunDTO {
	executionStatus := toolRun.ExecutionStatus
	if executionStatus == "" {
		if toolRun.ErrorCode != "" || !toolRun.Success {
			executionStatus = "failed"
		} else {
			executionStatus = "success"
		}
	}
	dataStatus := toolRun.DataStatus
	if dataStatus == "" {
		dataStatus = inferToolRunDataStatus(toolRun)
	}
	return dto.ToolRunDTO{
		Tool:               toolRun.Tool,
		Success:            toolRun.Success,
		DurationMS:         toolRun.DurationMS,
		ElapsedMS:          toolRun.DurationMS,
		ErrorCode:          string(toolRun.ErrorCode),
		ErrorMessage:       toolRun.ErrorMessage,
		EvidenceCount:      toolRun.EvidenceCount,
		WarningCount:       toolRun.WarningCount,
		EvidenceIDs:        append([]string(nil), toolRun.EvidenceIDs...),
		ExecutionStatus:    executionStatus,
		DataStatus:         dataStatus,
		FallbackUsed:       toolRun.FallbackUsed || toolRunMetadataFallback(toolRun.Metadata),
		NormalizedArgs:     toolRun.NormalizedArgs,
		NormalizedArgsHash: toolRun.NormalizedArgsHash,
		Service:            toolRun.Service,
		TimeRange:          mapCommonTimeRange(toolRun.TimeRange),
		ToolCategory:       toolRun.ToolCategory,
		Deduplicated:       toolRun.Deduplicated,
		ReusedResultFrom:   toolRun.ReusedResultFrom,
		RetryCount:         toolRun.RetryCount,
		RetryReason:        toolRun.RetryReason,
		Metadata:           toolRun.Metadata,
	}
}

func mapCommonTimeRange(value *common.TimeRange) *dto.TimeContext {
	if value == nil {
		return nil
	}
	return &dto.TimeContext{From: value.From, To: value.To}
}

func inferToolRunDataStatus(toolRun agenteino.ToolRun) string {
	if toolRunMetadataFallback(toolRun.Metadata) {
		return "fallback"
	}
	if toolRun.ErrorCode != "" {
		return "unknown"
	}
	if toolRun.EvidenceCount == 0 {
		return "empty"
	}
	if toolRun.WarningCount > 0 {
		return "partial"
	}
	return "available"
}

func toolRunMetadataFallback(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	if fallback, _ := metadata["fallback_used"].(bool); fallback {
		return true
	}
	if mode, _ := metadata["mode"].(string); strings.Contains(mode, "fallback") || strings.Contains(mode, "mock") {
		return true
	}
	return false
}

func cloneResponseMetadata(metadata map[string]any) map[string]any {
	cloned := map[string]any{}
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func ensureFinalDiagnosisMetadata(
	metadata map[string]any,
	output agenteino.AgentOutput,
	executionMode string,
) {
	if metadata == nil {
		return
	}
	if executionMode == "" {
		executionMode = "single_agent"
	}
	metadata["execution_mode"] = executionMode
	if _, exists := metadata["final_diagnosis"]; exists {
		if _, ok := metadata["final_diagnosis_schema_version"]; !ok {
			metadata["final_diagnosis_schema_version"] = "watchops.final_diagnosis.v1"
		}
		if diagnosis, ok := metadata["final_diagnosis"].(multiagent.FinalDiagnosis); ok {
			copyFinalStatusMetadata(metadata, diagnosis)
		}
		return
	}
	language := responseRequestedLanguage(metadata)
	diagnosis := adaptAgentOutputToFinalDiagnosis(output, language, executionMode)
	metadata["requested_language"] = language
	metadata["final_diagnosis"] = diagnosis
	metadata["final_diagnosis_schema_version"] = diagnosis.SchemaVersion
	copyFinalStatusMetadata(metadata, diagnosis)
}

func copyFinalStatusMetadata(metadata map[string]any, diagnosis multiagent.FinalDiagnosis) {
	for _, key := range []string{
		"execution_status",
		"llm_execution_status",
		"evidence_completeness",
		"diagnosis_status",
		"data_degraded",
		"limitation_count",
		"live_evidence_count",
		"knowledge_evidence_count",
		"long_term_memory_count",
		"fallback_evidence_count",
		"total_evidence_count",
	} {
		if value, ok := diagnosis.Metadata[key]; ok {
			metadata[key] = value
		}
	}
}

func responseRequestedLanguage(metadata map[string]any) string {
	for _, key := range []string{"requested_language", "language", "response_language"} {
		if value, ok := metadata[key].(string); ok {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "zh", "zh-cn", "zh_cn":
				return "zh-CN"
			case "en", "en-us", "en_us":
				return "en-US"
			}
		}
	}
	return "en-US"
}

func adaptAgentOutputToFinalDiagnosis(
	output agenteino.AgentOutput,
	language string,
	executionMode string,
) multiagent.FinalDiagnosis {
	evidenceRefs := adaptEvidenceReferences(output.Evidence, language)
	findings := dedupeFinalFindings(adaptFindings(output, language))
	limitations, executionWarnings := splitAgentLimitations(output.Limitations)
	limitations = dedupeFinalLimitations(limitations)
	executionWarnings = append(executionWarnings, executionWarningsFromAgentToolRuns(output.ToolRuns, language)...)
	executionWarnings = dedupeExecutionWarnings(executionWarnings)
	recommendations := adaptRecommendations(output, language)
	root := adaptRootCause(output, language)
	root = adjustAgentRootCause(root, output.Evidence, language)
	summary := finalDiagnosisSummary(output, language, len(evidenceRefs))
	evidenceCompleteness := agentEvidenceCompleteness(output.Evidence, limitations)
	dataDegraded := evidenceCompleteness != "complete" || len(executionWarnings) > 0
	diagnosisStatus := agentDiagnosisStatus(root, evidenceCompleteness)
	evidenceCounts := agentEvidenceOriginCounts(output.Evidence)
	return multiagent.FinalDiagnosis{
		SchemaVersion: "watchops.final_diagnosis.v1",
		Language:      language,
		ExecutionMode: executionMode,
		Summary:       summary,
		Incident: multiagent.IncidentOverview{
			Service:      inferService(output.Evidence),
			IncidentType: inferIncidentType(output),
			Severity:     inferSeverity(output),
			Status:       inferStatus(language, len(output.Evidence)),
		},
		Findings:          findings,
		RootCause:         root,
		Recommendations:   recommendations,
		Limitations:       limitations,
		ExecutionWarnings: executionWarnings,
		EvidenceRefs:      evidenceRefs,
		Metadata: map[string]any{
			"schema_version":           "watchops.final_diagnosis.v1",
			"adapted_from":             "agent_output",
			"execution_status":         agentExecutionStatus(dataDegraded, output.Metadata),
			"llm_execution_status":     agentLLMExecutionStatus(output.Metadata),
			"evidence_completeness":    evidenceCompleteness,
			"diagnosis_status":         diagnosisStatus,
			"data_degraded":            dataDegraded,
			"limitation_count":         len(limitations),
			"live_evidence_count":      evidenceCounts["live"],
			"knowledge_evidence_count": evidenceCounts["knowledge"],
			"long_term_memory_count":   evidenceCounts["historical_memory"],
			"fallback_evidence_count":  evidenceCounts["fallback"],
			"total_evidence_count":     evidenceCounts["total"],
		},
	}
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

func adaptFindings(output agenteino.AgentOutput, language string) []multiagent.FinalFinding {
	findings := make([]multiagent.FinalFinding, 0, len(output.Conclusions)+len(output.Inferences))
	for _, conclusion := range output.Conclusions {
		if strings.TrimSpace(conclusion.Text) == "" {
			continue
		}
		findings = append(findings, multiagent.FinalFinding{
			Title:       finalFindingTitle(conclusion.Text, "fact", language),
			Description: conclusion.Text,
			Confidence:  confidenceForEvidenceIDs(conclusion.EvidenceIDs),
			EvidenceIDs: validAgentEvidenceIDs(conclusion.EvidenceIDs, output.Evidence),
			Kind:        "fact",
		})
	}
	for _, inference := range output.Inferences {
		if strings.TrimSpace(inference.Text) == "" {
			continue
		}
		findings = append(findings, multiagent.FinalFinding{
			Title:       finalFindingTitle(inference.Text, "hypothesis", language),
			Description: inference.Text,
			Confidence:  confidenceForEvidenceIDs(inference.EvidenceIDs),
			EvidenceIDs: validAgentEvidenceIDs(inference.EvidenceIDs, output.Evidence),
			Kind:        "hypothesis",
		})
	}
	return findings
}

func adaptRootCause(output agenteino.AgentOutput, language string) multiagent.RootCauseAssessment {
	root := multiagent.RootCauseAssessment{
		Conclusion:   localizedFinalText(language, "证据不足，当前不能确认已观察到根因。", "Evidence is insufficient to confirm an observed root cause."),
		Confidence:   "low",
		EvidenceIDs:  []string{},
		Alternatives: []string{},
		Status:       "insufficient_evidence",
	}
	if len(output.Inferences) > 0 {
		inference := output.Inferences[0]
		root.Conclusion = inference.Text
		root.EvidenceIDs = validAgentEvidenceIDs(inference.EvidenceIDs, output.Evidence)
		root.Confidence = confidenceForEvidenceIDs(root.EvidenceIDs)
		root.Status = rootStatusForConfidence(root.Confidence)
		for _, item := range output.Inferences[1:] {
			if strings.TrimSpace(item.Text) != "" {
				root.Alternatives = append(root.Alternatives, item.Text)
			}
		}
	}
	return root
}

func adaptRecommendations(output agenteino.AgentOutput, language string) []multiagent.FinalRecommendation {
	result := make([]multiagent.FinalRecommendation, 0, len(output.Recommendations))
	for index, recommendation := range output.Recommendations {
		if strings.TrimSpace(recommendation.Text) == "" {
			continue
		}
		evidenceIDs := validAgentEvidenceIDs(recommendation.EvidenceIDs, output.Evidence)
		profile := agentRecommendationProfile(recommendation.Text, evidenceIDs, output.Evidence, language)
		result = append(result, multiagent.FinalRecommendation{
			Priority:     priorityForIndex(index),
			Action:       recommendation.Text,
			Reason:       profile.Reason,
			Risk:         profile.Risk,
			Verification: profile.Verification,
			EvidenceIDs:  evidenceIDs,
		})
	}
	return result
}

func finalDiagnosisSummary(output agenteino.AgentOutput, language string, evidenceCount int) string {
	if len(output.Conclusions) > 0 && strings.TrimSpace(output.Conclusions[0].Text) != "" {
		return output.Conclusions[0].Text
	}
	if evidenceCount == 0 {
		return localizedFinalText(language,
			"本次请求未获得可用证据，回答仅能给出调查边界。",
			"This request did not collect usable evidence, so the answer is limited to investigation boundaries.",
		)
	}
	return localizedFinalText(language,
		"已基于收集到的证据生成有边界的排障判断。",
		"An evidence-bound diagnostic assessment was generated from the collected evidence.",
	)
}

func finalFindingTitle(text string, kind string, language string) string {
	lower := strings.ToLower(text)
	switch {
	case kind == "hypothesis":
		return localizedFinalText(language, "待验证假设", "Hypothesis to verify")
	case strings.Contains(lower, "timeout") || strings.Contains(text, "超时"):
		return localizedFinalText(language, "超时信号", "Timeout signal")
	case strings.Contains(lower, "latency") || strings.Contains(text, "延迟"):
		return localizedFinalText(language, "延迟信号", "Latency signal")
	case strings.Contains(lower, "error") || strings.Contains(lower, "5xx") || strings.Contains(text, "错误"):
		return localizedFinalText(language, "错误率信号", "Error-rate signal")
	default:
		return localizedFinalText(language, "证据发现", "Evidence finding")
	}
}

func confidenceForEvidenceIDs(ids []string) string {
	switch {
	case len(ids) >= 2:
		return "high"
	case len(ids) == 1:
		return "medium"
	default:
		return "low"
	}
}

func rootStatusForConfidence(confidence string) string {
	switch confidence {
	case "high":
		return "likely"
	case "medium":
		return "possible"
	default:
		return "insufficient_evidence"
	}
}

func priorityForIndex(index int) string {
	switch index {
	case 0:
		return "P0"
	case 1:
		return "P1"
	default:
		return "P2"
	}
}

func localizedRecommendationReason(language string, evidenceIDs []string) string {
	if len(evidenceIDs) == 0 {
		return localizedFinalText(language,
			"该建议缺少直接证据引用，执行前需要补充验证。",
			"This recommendation has no direct evidence reference and needs validation before action.",
		)
	}
	return localizedFinalText(language,
		"该建议由引用证据支持，执行前仍需结合实时状态确认。",
		"This recommendation is supported by cited evidence and should still be checked against live state.",
	)
}

type agentRecommendationText struct {
	Reason       string
	Risk         string
	Verification string
}

func agentRecommendationProfile(
	action string,
	evidenceIDs []string,
	evidence []common.EvidenceItem,
	language string,
) agentRecommendationText {
	lower := strings.ToLower(action)
	if strings.Contains(lower, "payment") || strings.Contains(action, "支付") {
		return agentRecommendationText{
			Reason: localizedFinalText(language,
				"checkout 异常与 payment 依赖线索相关，但仍需要补齐当前依赖证据。",
				"Checkout symptoms correlate with payment dependency signals, but current dependency evidence still needs validation.",
			),
			Risk: localizedFinalText(language,
				"若只依据相关性判断，可能误把伴随现象当成因果根因。",
				"Acting on correlation alone may mistake a related symptom for the causal root cause.",
			),
			Verification: localizedFinalText(language,
				"对齐 payment p95/p99 延迟、超时计数、错误率与 checkout 5xx 的时间序列。",
				"Align payment p95/p99 latency, timeout counts, error rate, and checkout 5xx over the same window.",
			),
		}
	}
	if strings.Contains(lower, "retry") || strings.Contains(action, "重试") {
		return agentRecommendationText{
			Reason: localizedFinalText(language,
				"Runbook 或历史事件提示依赖延迟可能触发 checkout 重试放大。",
				"Runbook or historical evidence indicates dependency latency may trigger checkout retry amplification.",
			),
			Risk: localizedFinalText(language,
				"过度限制重试可能降低瞬时故障下的自动恢复能力。",
				"Over-limiting retries may reduce automatic recovery during transient failures.",
			),
			Verification: localizedFinalText(language,
				"比较调整前后的 checkout 重试次数、payment QPS、payment 延迟、checkout 5xx 和超时数量。",
				"Compare checkout retry count, payment QPS, payment latency, checkout 5xx, and timeout counts before and after the change.",
			),
		}
	}
	if len(evidenceIDs) == 0 {
		return agentRecommendationText{
			Reason: localizedFinalText(language,
				"该建议缺少直接证据引用，执行前需要补充观测数据。",
				"This recommendation has no direct evidence reference and needs more observations before action.",
			),
			Risk: localizedFinalText(language,
				"缺少依据时直接操作可能扩大影响面或修错组件。",
				"Acting without evidence may increase blast radius or repair the wrong component.",
			),
			Verification: localizedFinalText(language,
				"先补齐相关 metrics、logs、traces，再确认建议是否仍成立。",
				"Collect related metrics, logs, and traces first, then confirm whether the recommendation still holds.",
			),
		}
	}
	return agentRecommendationText{
		Reason: localizedFinalText(language,
			"该建议由当前证据支持，但仍需结合缺失观测确认执行条件。",
			"The recommendation is tied to current evidence, but missing observations should still be checked.",
		),
		Risk: localizedFinalText(language,
			"证据不完整时直接操作可能扩大影响面。",
			"Acting with incomplete evidence may increase blast radius.",
		),
		Verification: localizedFinalText(language,
			"执行前后对比相关 metrics、logs、traces 与告警状态。",
			"Compare related metrics, logs, traces, and alert state before and after action.",
		),
	}
}

func localizedFinalText(language string, zh string, en string) string {
	if strings.EqualFold(language, "zh-CN") || strings.EqualFold(language, "zh") {
		return zh
	}
	return en
}

func nonEmptyString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func adjustAgentRootCause(
	root multiagent.RootCauseAssessment,
	evidence []common.EvidenceItem,
	language string,
) multiagent.RootCauseAssessment {
	if root.Status == "insufficient_evidence" {
		return root
	}
	hasLogs := hasSourceType(evidence, "logs")
	hasTraces := hasSourceType(evidence, "traces")
	hasDependencyMetrics := hasDependencyMetric(evidence)
	if !hasLogs && !hasTraces && root.Confidence == "high" {
		root.Confidence = "medium"
		if root.Status == "confirmed" {
			root.Status = "likely"
		}
	}
	if !hasDependencyMetrics {
		root.Status = "possible"
		root.Confidence = minAgentConfidence(root.Confidence, "medium")
		if !strings.Contains(root.Conclusion, "缺少") && !strings.Contains(strings.ToLower(root.Conclusion), "missing") {
			root.Conclusion += localizedFinalText(language,
				"，但当前时间窗口缺少依赖服务实时指标，暂时只能作为可能根因。",
				", but current dependency metrics are missing, so this remains a possible root cause.",
			)
		}
	}
	return root
}

func hasSourceType(evidence []common.EvidenceItem, source string) bool {
	for _, item := range evidence {
		if item.SourceType == source {
			return true
		}
	}
	return false
}

func hasDependencyMetric(evidence []common.EvidenceItem) bool {
	primary := inferService(evidence)
	for _, item := range evidence {
		if item.SourceType != "metrics" {
			continue
		}
		service := strings.TrimSpace(item.ResourceID)
		if value, ok := item.Metadata["service"].(string); ok && strings.TrimSpace(value) != "" {
			service = strings.TrimSpace(value)
		}
		if service != "" && service != primary {
			return true
		}
	}
	return false
}

func minAgentConfidence(current string, maximum string) string {
	rank := map[string]int{"low": 0, "medium": 1, "high": 2}
	if _, ok := rank[current]; !ok {
		return maximum
	}
	if rank[current] > rank[maximum] {
		return maximum
	}
	return current
}

func agentEvidenceCompleteness(evidence []common.EvidenceItem, limitations []multiagent.FinalLimitation) string {
	realEvidence := make([]common.EvidenceItem, 0, len(evidence))
	for _, item := range evidence {
		if finalEvidenceDataStatus(item) != "fallback" {
			realEvidence = append(realEvidence, item)
		}
	}
	if len(realEvidence) == 0 {
		return "empty"
	}
	if len(limitations) > 0 || !hasSourceType(realEvidence, "logs") || !hasSourceType(realEvidence, "traces") {
		return "partial"
	}
	return "complete"
}

func agentLLMExecutionStatus(metadata map[string]any) string {
	if metadata == nil {
		return "unknown"
	}
	if fallback, _ := metadata["fallback_used"].(bool); fallback {
		return "fallback"
	}
	if mode, _ := metadata["agent_mode"].(string); mode == "eino_react" {
		return "success"
	}
	return "unknown"
}

func localizedToolWarning(toolName string, kind string, language string) string {
	if kind == "fallback" {
		return localizedFinalText(language,
			toolName+" 使用了 fallback 或演示数据，调用成功但数据质量已降级。",
			toolName+" used fallback or demo data; invocation succeeded but data quality is degraded.",
		)
	}
	return localizedFinalText(language,
		toolName+" 返回了部分数据或警告，结果需要谨慎解释。",
		toolName+" returned partial data or warnings and should be interpreted carefully.",
	)
}

func splitAgentLimitations(items []agenteino.Limitation) ([]multiagent.FinalLimitation, []multiagent.ExecutionWarning) {
	limitations := []multiagent.FinalLimitation{}
	warnings := []multiagent.ExecutionWarning{}
	for _, item := range items {
		code := strings.TrimSpace(item.Code)
		description := strings.TrimSpace(item.Message)
		if description == "" {
			description = code
		}
		if code == "" && description == "" {
			continue
		}
		if isAgentExecutionWarningCode(code) {
			warnings = append(warnings, multiagent.ExecutionWarning{
				Code:        nonEmptyString(code, "EXECUTION_WARNING"),
				Description: description,
				Source:      item.Tool,
			})
			continue
		}
		limitations = append(limitations, multiagent.FinalLimitation{
			Code:        nonEmptyString(code, "LIMITATION"),
			Description: description,
			Source:      item.Tool,
		})
	}
	return limitations, warnings
}

func isAgentExecutionWarningCode(code string) bool {
	upper := strings.ToUpper(strings.TrimSpace(code))
	return strings.HasPrefix(upper, "AGENT_") ||
		strings.Contains(upper, "MAX_ITERATION") ||
		strings.Contains(upper, "REPEATED_TOOL")
}

func dedupeFinalLimitations(items []multiagent.FinalLimitation) []multiagent.FinalLimitation {
	result := []multiagent.FinalLimitation{}
	seen := map[string]int{}
	for _, item := range items {
		item.Code = canonicalLimitationCode(item.Code, item.Source)
		key := item.Code + "|" + item.Source
		if key == "||" {
			continue
		}
		if existingIndex, exists := seen[key]; exists {
			if len(item.Description) > len(result[existingIndex].Description) {
				result[existingIndex].Description = item.Description
			}
			continue
		}
		seen[key] = len(result)
		result = append(result, item)
	}
	return result
}

func canonicalLimitationCode(code string, source string) string {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if !strings.Contains(upper, "NO_DATA") {
		return upper
	}
	combined := strings.ToUpper(source + " " + upper)
	switch {
	case strings.Contains(combined, "LOG"):
		return "LOGS_NO_DATA"
	case strings.Contains(combined, "TRACE"):
		return "TRACES_NO_DATA"
	case strings.Contains(combined, "METRIC"):
		return "METRICS_NO_DATA"
	case strings.Contains(combined, "KNOWLEDGE"):
		return "KNOWLEDGE_NO_DATA"
	case strings.Contains(combined, "ALERT"):
		return "ALERTS_NO_DATA"
	case strings.Contains(combined, "TOPOLOGY"):
		return "TOPOLOGY_NO_DATA"
	default:
		return upper
	}
}

func dedupeExecutionWarnings(items []multiagent.ExecutionWarning) []multiagent.ExecutionWarning {
	result := []multiagent.ExecutionWarning{}
	seen := map[string]struct{}{}
	for _, item := range items {
		key := item.Code + "|" + item.Description + "|" + item.Source
		if key == "||" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func executionWarningsFromAgentToolRuns(toolRuns []agenteino.ToolRun, language string) []multiagent.ExecutionWarning {
	result := []multiagent.ExecutionWarning{}
	for _, run := range toolRuns {
		switch inferToolRunDataStatus(run) {
		case "fallback":
			result = append(result, multiagent.ExecutionWarning{
				Code:        "TOOL_FALLBACK_USED",
				Description: localizedToolWarning(run.Tool, "fallback", language),
				Source:      run.Tool,
			})
		case "partial":
			result = append(result, multiagent.ExecutionWarning{
				Code:        "TOOL_DATA_PARTIAL",
				Description: localizedToolWarning(run.Tool, "partial", language),
				Source:      run.Tool,
			})
		}
	}
	return result
}

func evidenceReferenceTitle(item common.EvidenceItem) string {
	sourceType := strings.TrimSpace(item.SourceType)
	if sourceType == "" {
		sourceType = "evidence"
	}
	if resourceID := strings.TrimSpace(item.ResourceID); resourceID != "" {
		return sourceType + ": " + resourceID
	}
	return sourceType + ": " + item.ID
}

func localizedEvidenceInterpretation(sourceType string, language string) string {
	switch strings.TrimSpace(sourceType) {
	case "metrics":
		return localizedFinalText(language,
			"指标证据显示与当前故障相关的运行状态。",
			"Metric evidence shows runtime state related to the incident.",
		)
	case "logs":
		return localizedFinalText(language,
			"日志证据提供了与请求或错误相关的原始事件。",
			"Log evidence provides raw events related to requests or errors.",
		)
	case "traces":
		return localizedFinalText(language,
			"Trace 证据提供了调用路径或耗时线索。",
			"Trace evidence provides call-path or latency clues.",
		)
	case "knowledge":
		return localizedFinalText(language,
			"知识库证据提供了 runbook 或历史处理指导。",
			"Knowledge evidence provides runbook or historical guidance.",
		)
	default:
		return localizedFinalText(language,
			"该证据用于支撑诊断判断。",
			"This evidence supports the diagnostic assessment.",
		)
	}
}

func adaptEvidenceReferences(items []common.EvidenceItem, language string) []multiagent.EvidenceReference {
	result := make([]multiagent.EvidenceReference, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		result = append(result, multiagent.EvidenceReference{
			ID:                    item.ID,
			Type:                  item.SourceType,
			Title:                 evidenceReferenceTitle(item),
			RawExcerpt:            item.Content,
			Interpretation:        localizedEvidenceInterpretation(item.SourceType, language),
			SourceLabel:           evidenceSourceLabel(item),
			Source:                item.SourceName,
			DataStatus:            finalEvidenceDataStatus(item),
			EvidenceWeight:        finalEvidenceWeight(item),
			EvidenceOrigin:        finalEvidenceOrigin(item),
			CanConfirmCurrentFact: finalEvidenceCanConfirmCurrentFact(item),
			CanSupportHypothesis:  finalEvidenceCanSupportHypothesis(item),
			SupportsRootCause:     finalEvidenceSupportsRootCause(item),
		})
	}
	return result
}

func finalEvidenceDataStatus(item common.EvidenceItem) string {
	if item.Metadata != nil {
		if status, _ := item.Metadata["data_status"].(string); status != "" {
			return status
		}
		if fallback, _ := item.Metadata["fallback_used"].(bool); fallback {
			return "fallback"
		}
		if mode, _ := item.Metadata["mode"].(string); strings.Contains(strings.ToLower(mode), "fallback") || strings.Contains(strings.ToLower(mode), "mock") {
			return "fallback"
		}
	}
	return "available"
}

func finalEvidenceWeight(item common.EvidenceItem) string {
	if finalEvidenceDataStatus(item) == "fallback" {
		return "contextual"
	}
	switch finalEvidenceOrigin(item) {
	case "knowledge", "historical_memory", "fallback":
		return "contextual"
	}
	if item.SourceType == "knowledge" || item.SourceType == "topology" ||
		item.SourceType == "memory" || item.SourceType == "long_term_memory" {
		return "contextual"
	}
	return "primary"
}

func finalEvidenceOrigin(item common.EvidenceItem) string {
	if item.Metadata != nil {
		if origin, _ := item.Metadata["evidence_origin"].(string); origin != "" {
			return origin
		}
	}
	if finalEvidenceDataStatus(item) == "fallback" {
		return "fallback"
	}
	switch strings.ToLower(strings.TrimSpace(item.SourceType)) {
	case "metrics", "logs", "traces", "alerts", "topology":
		return "live"
	case "knowledge":
		return "knowledge"
	case "memory", "long_term_memory":
		return "historical_memory"
	default:
		return "inferred"
	}
}

func finalEvidenceCanConfirmCurrentFact(item common.EvidenceItem) bool {
	return finalEvidenceOrigin(item) == "live" && finalEvidenceDataStatus(item) != "fallback"
}

func finalEvidenceCanSupportHypothesis(item common.EvidenceItem) bool {
	switch finalEvidenceOrigin(item) {
	case "live", "knowledge", "historical_memory":
		return true
	default:
		return false
	}
}

func finalEvidenceSupportsRootCause(item common.EvidenceItem) bool {
	return finalEvidenceWeight(item) == "primary" && finalEvidenceDataStatus(item) != "fallback"
}

func agentEvidenceOriginCounts(items []common.EvidenceItem) map[string]int {
	counts := map[string]int{
		"live":              0,
		"knowledge":         0,
		"historical_memory": 0,
		"fallback":          0,
		"inferred":          0,
		"total":             0,
	}
	for _, item := range items {
		origin := finalEvidenceOrigin(item)
		counts[origin]++
		if origin != "inferred" {
			counts["total"]++
		}
	}
	return counts
}

func agentDiagnosisStatus(root multiagent.RootCauseAssessment, completeness string) string {
	if completeness == "empty" {
		return "insufficient_evidence"
	}
	if completeness == "partial" && root.Status == "confirmed" {
		return "supported"
	}
	if root.Status != "" {
		return root.Status
	}
	if root.Confidence == "high" && completeness == "complete" {
		return "confirmed"
	}
	if root.Confidence == "medium" {
		return "supported"
	}
	return "hypothesis_only"
}

func agentExecutionStatus(dataDegraded bool, metadata map[string]any) string {
	if fallback, _ := metadata["fallback_used"].(bool); fallback {
		return "degraded"
	}
	if reached, _ := metadata["agent_max_iterations_reached"].(bool); reached {
		return "degraded"
	}
	if dataDegraded {
		return "degraded"
	}
	return "success"
}

func evidenceSourceLabel(item common.EvidenceItem) string {
	if title, ok := item.Metadata["title"].(string); ok && strings.TrimSpace(title) != "" {
		return title
	}
	switch item.SourceType {
	case "knowledge":
		return "Knowledge RAG"
	case "memory", "long_term_memory":
		return "Long-term memory"
	default:
		if item.SourceName != "" {
			return item.SourceName
		}
		return item.SourceType
	}
}

func inferService(evidence []common.EvidenceItem) string {
	for _, item := range evidence {
		if value, ok := item.Metadata["service"].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
		if resourceID := strings.TrimSpace(item.ResourceID); resourceID != "" {
			return resourceID
		}
	}
	return "unknown"
}

func inferIncidentType(output agenteino.AgentOutput) string {
	text := strings.ToLower(joinAgentOutputText(output))
	switch {
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline") || strings.Contains(text, "超时"):
		return "timeout"
	case strings.Contains(text, "latency") || strings.Contains(text, "slow") || strings.Contains(text, "延迟"):
		return "latency"
	case strings.Contains(text, "error") || strings.Contains(text, "5xx") || strings.Contains(text, "错误"):
		return "high_error_rate"
	default:
		return "unknown"
	}
}

func inferSeverity(output agenteino.AgentOutput) string {
	switch inferIncidentType(output) {
	case "timeout", "high_error_rate":
		return "high"
	case "latency":
		return "medium"
	default:
		return "unknown"
	}
}

func inferStatus(language string, evidenceCount int) string {
	if evidenceCount == 0 {
		return "insufficient_evidence"
	}
	return "investigating"
}

func joinAgentOutputText(output agenteino.AgentOutput) string {
	parts := []string{}
	for _, item := range output.Conclusions {
		parts = append(parts, item.Text)
	}
	for _, item := range output.Inferences {
		parts = append(parts, item.Text)
	}
	for _, item := range output.Recommendations {
		parts = append(parts, item.Text)
	}
	for _, item := range output.Evidence {
		parts = append(parts, item.Content)
	}
	return strings.Join(parts, " ")
}

func dedupeFinalFindings(items []multiagent.FinalFinding) []multiagent.FinalFinding {
	result := []multiagent.FinalFinding{}
	seen := map[string]struct{}{}
	for _, item := range items {
		key := item.Kind + "|" + strings.Join(item.EvidenceIDs, ",") + "|" + strings.ToLower(strings.TrimSpace(item.Description))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func validAgentEvidenceIDs(ids []string, evidence []common.EvidenceItem) []string {
	valid := map[string]struct{}{}
	for _, item := range evidence {
		if item.ID != "" {
			valid[item.ID] = struct{}{}
		}
	}
	result := []string{}
	seen := map[string]struct{}{}
	for _, id := range ids {
		if _, ok := valid[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
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
