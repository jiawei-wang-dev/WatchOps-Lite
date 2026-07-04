package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	sessionSummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/multiagent"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRouterServesHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newTestRouter(t)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "req-test")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("X-Request-ID"); got != "req-test" {
		t.Fatalf("X-Request-ID = %q, want req-test", got)
	}

	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ok" || body.Service != "watchops-lite" {
		t.Fatalf("response = %#v, want healthy watchops-lite service", body)
	}
}

func TestRouterServesEmbeddedDemoConsole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)

	indexRecorder := httptest.NewRecorder()
	router.ServeHTTP(indexRecorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if indexRecorder.Code != http.StatusOK {
		t.Fatalf("index status = %d, want %d", indexRecorder.Code, http.StatusOK)
	}
	if contentType := indexRecorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("index Content-Type = %q, want HTML", contentType)
	}
	if !strings.Contains(indexRecorder.Body.String(), "WatchOps-Lite 智能排障控制台") {
		t.Fatalf("index does not contain console title")
	}
	for _, expected := range []string{
		"load-history",
		"clear-history",
		"history-messages",
		`data-lang="zh"`,
		`data-lang="en"`,
		`src="/web/i18n.js"`,
		`id="stream-summary"`,
		`id="stream-key-steps"`,
		`id="stream-tool-timeline"`,
		`id="stream-key-evidence"`,
		`id="raw-sse-details"`,
		`id="key-evidence-groups"`,
		`id="full-evidence-details"`,
		`class="trace-guide-card"`,
		`id="status-eino"`,
		`id="status-tools"`,
		`id="status-redis"`,
		`id="status-mysql"`,
		`id="status-sse"`,
		`id="status-llm"`,
		`class="status-help"`,
		`class="hero-top-row"`,
		`class="hero-console-title"`,
		`class="runtime-status-area"`,
		`data-agent-mode="single"`,
		`data-agent-mode="multi"`,
		`id="multi-agent-panel"`,
		`id="multi-agent-steps"`,
	} {
		if !strings.Contains(indexRecorder.Body.String(), expected) {
			t.Fatalf("index does not contain console control %q", expected)
		}
	}

	assetRecorder := httptest.NewRecorder()
	router.ServeHTTP(assetRecorder, httptest.NewRequest(http.MethodGet, "/web/app.js", nil))
	if assetRecorder.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want %d", assetRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(assetRecorder.Body.String(), "sendStreamChat") {
		t.Fatalf("JavaScript asset does not contain streaming client")
	}
	for _, expected := range []string{
		"/api/v1/chat/history",
		"loadHistory",
		"clearHistory",
		"WatchOpsI18n",
		"renderStreamSummary",
		"renderStreamSteps",
		"renderStreamToolTimeline",
		"renderKeyEvidenceGroups",
		"latestStreamResponse",
		"renderKnowledgeCard",
		"findDuplicateKnowledgeResults",
		"renderRuntimeStatuses",
		"setRuntimeStatus",
		"/api/v1/chat/multi-agent",
		"/api/v1/chat/multi-agent/stream",
		"renderMultiAgentSteps",
	} {
		if !strings.Contains(assetRecorder.Body.String(), expected) {
			t.Fatalf("JavaScript asset does not contain history integration %q", expected)
		}
	}

	i18nRecorder := httptest.NewRecorder()
	router.ServeHTTP(i18nRecorder, httptest.NewRequest(http.MethodGet, "/web/i18n.js", nil))
	if i18nRecorder.Code != http.StatusOK {
		t.Fatalf("i18n asset status = %d, want %d", i18nRecorder.Code, http.StatusOK)
	}
	for _, expected := range []string{
		"zh:",
		"en:",
		`"nav.chat"`,
		`"chat.send"`,
		`"stream.execution_summary"`,
		`"stream.key_steps"`,
		`"stream.tool_timeline"`,
		`"stream.raw_events"`,
		`"trace.guide_title"`,
		`"knowledge.duplicate_seed"`,
		`"knowledge.duplicates_hidden"`,
		`"knowledge.raw_content"`,
		`"status.redis_active"`,
		`"status.llm_fallback"`,
		`"status.why_not_active"`,
		`"multi.role_triage"`,
		`"multi.role_synthesis"`,
		`"event.multi_agent_completed"`,
		"localStorage.setItem",
	} {
		if !strings.Contains(i18nRecorder.Body.String(), expected) {
			t.Fatalf("i18n asset does not contain %q", expected)
		}
	}
}

func TestDemoConsoleDoesNotShadowAPIRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/not-a-console-asset", nil),
	)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if strings.Contains(recorder.Body.String(), "WatchOps-Lite 智能排障控制台") {
		t.Fatalf("unknown API route was shadowed by the demo console")
	}
}

func TestRouterServesRuntimeMetricsWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	collector := runtimemetrics.New()
	runtimemetrics.SetDefault(collector)
	t.Cleanup(func() { runtimemetrics.SetDefault(nil) })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router := NewRouter(
		logger,
		"watchops-lite",
		RouterDependencies{Metrics: collector.Handler()},
	)

	healthRecorder := httptest.NewRecorder()
	router.ServeHTTP(
		healthRecorder,
		httptest.NewRequest(http.MethodGet, "/healthz", nil),
	)
	metricsRecorder := httptest.NewRecorder()
	router.ServeHTTP(
		metricsRecorder,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)

	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", metricsRecorder.Code, http.StatusOK)
	}
	if !bytes.Contains(metricsRecorder.Body.Bytes(), []byte("watchops_http_requests_total")) {
		t.Fatalf("metrics response does not contain HTTP request metric: %s", metricsRecorder.Body.String())
	}
}

func TestRouterOmitsRuntimeMetricsWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router := NewRouter(logger, "watchops-lite", RouterDependencies{})
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestRouterRecoversFromPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newTestRouter(t)
	router.GET("/panic", func(_ *gin.Context) {
		panic("test panic")
	})

	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want JSON", contentType)
	}
}

func TestRouterServesChatForErrorRateQuestion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)

	requestBody := []byte(`{
		"session_id": "ses_01",
		"message": "Why did checkout error rate increase in the last 20 minutes?",
		"time_context": {
			"from": "2026-06-30T00:00:00Z",
			"to": "2026-06-30T00:20:00Z"
		}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req-chat-test")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response dto.ChatResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.RequestID != "req-chat-test" || response.SessionID != "ses_01" {
		t.Fatalf("response IDs = %#v, want request and session IDs preserved", response)
	}
	if len(response.Answer.Conclusion) == 0 ||
		len(response.Answer.Evidence) == 0 ||
		len(response.Answer.Recommendations) == 0 ||
		len(response.Answer.Limitations) == 0 {
		t.Fatalf("answer is missing required populated sections: %#v", response.Answer)
	}
	if response.Metadata["session_memory_available"] != true {
		t.Fatalf("metadata = %#v, want available session memory", response.Metadata)
	}

	assertToolRuns(t, response.ToolRuns, "query_metrics", "query_logs")
}

func TestRouterServesOptionalMultiAgentMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)
	requestBody := []byte(`{
		"session_id": "multi_01",
		"message": "Why did checkout error rate increase? Include metrics, logs, alerts, and runbook evidence.",
		"time_context": {
			"from": "2026-06-30T00:00:00Z",
			"to": "2026-06-30T00:20:00Z"
		}
	}`)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/chat/multi-agent",
		bytes.NewReader(requestBody),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req-multi-test")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body=%s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}
	var response dto.MultiAgentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.RequestID != "req-multi-test" ||
		response.SessionID != "multi_01" ||
		response.Mode != "multi_agent" {
		t.Fatalf("response IDs/mode = %#v", response)
	}
	if response.Metadata["agent_mode"] != "multi_agent" ||
		response.Metadata["orchestrator"] != "eino_graph" ||
		len(response.AgentSteps) != 4 ||
		len(response.Answer.Conclusion) == 0 ||
		len(response.Evidence) == 0 {
		t.Fatalf("multi-agent response = %#v", response)
	}
}

func TestRouterServesChineseChatWithoutChangingSchemaOrToolNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)
	requestBody := []byte(`{
		"session_id": "ses_zh_01",
		"message": "checkout 服务错误率为什么升高？请结合指标和日志分析。",
		"time_context": {
			"from": "2026-06-30T00:00:00Z",
			"to": "2026-06-30T00:20:00Z"
		}
	}`)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/chat",
		bytes.NewReader(requestBody),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req-chat-zh-test")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want UTF-8 JSON", contentType)
	}
	var response dto.ChatResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode Chinese response: %v", err)
	}
	if response.RequestID != "req-chat-zh-test" ||
		len(response.Answer.Conclusion) == 0 ||
		!strings.Contains(response.Answer.Conclusion[0].Text, "证据") {
		t.Fatalf("Chinese response = %#v", response)
	}
	assertToolRuns(t, response.ToolRuns, "query_metrics", "query_logs")
	for _, item := range response.Answer.Evidence {
		if strings.ContainsAny(item.ID, "支付错误率日志指标") {
			t.Fatalf("evidence ID was translated: %q", item.ID)
		}
	}
}

func TestRouterStreamsChatEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)

	requestBody := []byte(`{
		"session_id": "ses_stream",
		"message": "Why did checkout error rate increase in the last 20 minutes?",
		"time_context": {
			"from": "2026-06-30T00:00:00Z",
			"to": "2026-06-30T00:20:00Z"
		}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/chat/stream", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req-stream-test")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", contentType)
	}

	body := recorder.Body.String()
	for _, expected := range []string{
		"event: workflow_started",
		"event: graph_node_started",
		"event: graph_node_completed",
		`"node":"load_session_context"`,
		`"node":"load_long_term_memory"`,
		`"node":"prepare_diagnostic_skills"`,
		`"node":"merge_context"`,
		"event: tool_call_started",
		"event: tool_call_completed",
		"event: evidence_collected",
		"event: final_answer",
		"event: workflow_completed",
		`"request_id":"req-stream-test"`,
		`"session_id":"ses_stream"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("stream body missing %q:\n%s", expected, body)
		}
	}

	finalIndex := strings.Index(body, "event: final_answer")
	completedIndex := strings.Index(body, "event: workflow_completed")
	if finalIndex < 0 || completedIndex < 0 || finalIndex > completedIndex {
		t.Fatalf("final_answer must appear before workflow_completed:\n%s", body)
	}
	for _, forbidden := range []string{"chain_of_thought", "raw_prompt", "private reasoning"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("stream body contains forbidden reasoning or prompt field %q:\n%s", forbidden, body)
		}
	}
}

func TestRouterGetsBoundedChatHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &routerSessionStore{
		messages: []session.Message{
			{
				Role:      session.RoleUser,
				Content:   "first",
				CreatedAt: time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC),
				RequestID: "req-1",
			},
			{
				Role:      session.RoleAssistant,
				Content:   "second",
				CreatedAt: time.Date(2026, 7, 3, 1, 1, 0, 0, time.UTC),
				RequestID: "req-1",
				Metadata:  map[string]any{"evidence_ids": []string{"ev-1"}},
			},
		},
		summary: session.Summary{
			Content:        "checkout investigation",
			Version:        2,
			ConfirmedFacts: []string{"checkout errors increased"},
		},
	}
	router := newTestRouterWithStore(t, store)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/chat/history?session_id=ses-history&limit=1",
			nil,
		),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response dto.ChatHistoryResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.SessionID != "ses-history" ||
		response.Limit != 1 ||
		response.Count != 1 ||
		response.Messages[0].Content != "second" ||
		response.Summary.Version != 2 {
		t.Fatalf("response = %#v", response)
	}
}

func TestRouterValidatesChatHistoryQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)
	tests := []string{
		"/api/v1/chat/history",
		"/api/v1/chat/history?session_id=ses&limit=invalid",
		"/api/v1/chat/history?session_id=ses&limit=-1",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(
				recorder,
				httptest.NewRequest(http.MethodGet, target, nil),
			)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf(
					"status = %d, want %d; body=%s",
					recorder.Code,
					http.StatusBadRequest,
					recorder.Body.String(),
				)
			}
		})
	}
}

func TestRouterCapsChatHistoryLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/chat/history?session_id=ses&limit=1000",
			nil,
		),
	)

	var response dto.ChatHistoryResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if recorder.Code != http.StatusOK || response.Limit != 100 {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}
}

func TestRouterDeletesChatHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &routerSessionStore{
		messages: []session.Message{{Role: session.RoleUser, Content: "message"}},
		summary:  session.Summary{Content: "summary", Version: 1},
	}
	router := newTestRouterWithStore(t, store)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodDelete,
			"/api/v1/chat/history?session_id=ses-history",
			nil,
		),
	)

	if recorder.Code != http.StatusOK || !store.cleared {
		t.Fatalf(
			"status=%d cleared=%v body=%s",
			recorder.Code,
			store.cleared,
			recorder.Body.String(),
		)
	}
	var response dto.ClearChatHistoryResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.SessionID != "ses-history" || !response.Cleared {
		t.Fatalf("response = %#v", response)
	}
	if len(store.messages) != 0 || store.summary.Version != 0 {
		t.Fatalf("store retained session history: %#v", store)
	}
}

func TestRouterCreatesEndToEndChatTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(noop.NewTracerProvider())
	})
	router := newTestRouter(t)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/chat",
		bytes.NewBufferString(`{
			"session_id":"ses_trace",
			"message":"Why did checkout error rate increase?",
			"time_context":{
				"from":"2026-06-30T00:00:00Z",
				"to":"2026-06-30T00:20:00Z"
			}
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response dto.ChatResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.TraceID == "" || recorder.Header().Get("X-Trace-ID") != response.TraceID {
		t.Fatalf("header trace ID=%q response trace ID=%q", recorder.Header().Get("X-Trace-ID"), response.TraceID)
	}

	names := map[string]bool{}
	for _, span := range exporter.GetSpans() {
		names[span.Name] = true
		if got := span.SpanContext.TraceID().String(); got != response.TraceID {
			t.Fatalf("span %q trace ID = %q, want %q", span.Name, got, response.TraceID)
		}
	}
	for _, expected := range []string{
		"HTTP POST /api/v1/chat",
		"chat.execute",
		"session.load_context",
		"agent.run",
		"tool.query_metrics",
		"tool.query_logs",
		"session.persist_context",
	} {
		if !names[expected] {
			t.Fatalf("spans = %#v, missing %q", names, expected)
		}
	}
}

func TestRouterRejectsInvalidChatRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req-invalid")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var response dto.ErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Code != "INVALID_ARGUMENT" || response.Error.RequestID != "req-invalid" {
		t.Fatalf("error response = %#v, want consistent invalid-argument response", response)
	}
}

func TestRouterRepresentsToolFailureInLimitations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)

	requestBody := []byte(`{
		"session_id": "ses_02",
		"message": "error rate increased",
		"time_context": {
			"from": "2026-06-30T00:00:00Z",
			"to": "2026-06-30T00:20:00Z"
		}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response dto.ChatResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	failedRuns := 0
	for _, run := range response.ToolRuns {
		if !run.Success && run.ErrorCode == "TOOL_INVALID_ARGUMENT" {
			failedRuns++
		}
	}
	if failedRuns != 2 {
		t.Fatalf("failed tool runs = %d, want 2; runs=%#v", failedRuns, response.ToolRuns)
	}

	toolLimitations := 0
	for _, limitation := range response.Answer.Limitations {
		if limitation.Tool != "" && limitation.Code == "TOOL_INVALID_ARGUMENT" {
			toolLimitations++
		}
	}
	if toolLimitations != 2 {
		t.Fatalf("tool limitations = %d, want 2; limitations=%#v", toolLimitations, response.Answer.Limitations)
	}
}

func TestRouterHandlesMalformedChatJSONWithoutPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouter(t)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(`{"session_id":`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestRouterChatSucceedsWhenSessionMemoryIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newTestRouterWithStore(t, &routerSessionStore{
		loadErr: errors.New("redis connection refused"),
	})

	requestBody := []byte(`{
		"session_id": "ses_03",
		"message": "Why did checkout error rate increase?",
		"time_context": {
			"from": "2026-06-30T00:00:00Z",
			"to": "2026-06-30T00:20:00Z"
		}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response dto.ChatResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Metadata["session_memory_available"] != false {
		t.Fatalf("metadata = %#v, want unavailable session memory", response.Metadata)
	}
	for _, limitation := range response.Answer.Limitations {
		if limitation.Code == "SESSION_MEMORY_UNAVAILABLE" {
			return
		}
	}
	t.Fatalf("limitations = %#v, want SESSION_MEMORY_UNAVAILABLE", response.Answer.Limitations)
}

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	return newTestRouterWithStore(t, &routerSessionStore{})
}

func newTestRouterWithStore(t *testing.T, store session.Store) *gin.Engine {
	t.Helper()

	tools, err := agenteino.BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}
	runner := agenteino.NewDeterministicRunner(tools)
	chatService := applicationchat.NewService(
		runner,
		store,
		sessionSummary.NewDeterministic(),
		applicationchat.ServiceConfig{
			RecentWindowSize: 12,
			SummaryThreshold: 12,
		},
	)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	evidenceAgent, err := multiagent.NewEvidenceAgent(context.Background(), tools)
	if err != nil {
		t.Fatalf("NewEvidenceAgent() error = %v", err)
	}
	knowledgeAgent, err := multiagent.NewKnowledgeAgent(
		context.Background(),
		tools,
		nil,
		3,
	)
	if err != nil {
		t.Fatalf("NewKnowledgeAgent() error = %v", err)
	}
	multiAgentService := multiagent.NewService(multiagent.NewOrchestrator(
		context.Background(),
		multiagent.NewDeterministicTriageAgent("checkout"),
		evidenceAgent,
		knowledgeAgent,
		multiagent.NewSynthesisAgent(nil),
	))

	return NewRouter(
		logger,
		"watchops-lite",
		RouterDependencies{
			Chat:       chatService,
			MultiAgent: multiAgentService,
		},
	)
}

type routerSessionStore struct {
	messages []session.Message
	summary  session.Summary
	loadErr  error
	cleared  bool
}

func (s *routerSessionStore) AppendMessage(
	_ context.Context,
	_ string,
	message session.Message,
) error {
	s.messages = append(s.messages, message)
	if len(s.messages) > 12 {
		s.messages = s.messages[len(s.messages)-12:]
	}
	return nil
}

func (s *routerSessionStore) GetRecentMessages(
	context.Context,
	string,
	int,
) ([]session.Message, error) {
	return s.messages, nil
}

func (s *routerSessionStore) GetSummary(context.Context, string) (session.Summary, error) {
	if s.summary.ConfirmedFacts == nil {
		return session.EmptySummary(), nil
	}
	return s.summary, nil
}

func (s *routerSessionStore) UpdateSummary(
	_ context.Context,
	_ string,
	summary session.Summary,
	expectedVersion int64,
) error {
	summary.Version = expectedVersion + 1
	s.summary = summary
	return nil
}

func (s *routerSessionStore) LoadContext(
	ctx context.Context,
	sessionID string,
) (session.ContextSnapshot, error) {
	if s.loadErr != nil {
		return session.ContextSnapshot{}, s.loadErr
	}
	summary, err := s.GetSummary(ctx, sessionID)
	if err != nil {
		return session.ContextSnapshot{}, err
	}
	return session.ContextSnapshot{
		Summary:        summary,
		RecentMessages: append([]session.Message{}, s.messages...),
	}, nil
}

func (s *routerSessionStore) ClearHistory(context.Context, string) error {
	s.messages = []session.Message{}
	s.summary = session.EmptySummary()
	s.cleared = true
	return nil
}

func assertToolRuns(t *testing.T, runs []dto.ToolRunDTO, expectedNames ...string) {
	t.Helper()

	found := make(map[string]bool, len(runs))
	for _, run := range runs {
		found[run.Tool] = true
	}
	for _, name := range expectedNames {
		if !found[name] {
			t.Fatalf("tool_runs = %#v, missing %q", runs, name)
		}
	}
}
