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

	"github.com/gin-gonic/gin"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	sessionSummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
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

	return NewRouter(
		logger,
		"watchops-lite",
		RouterDependencies{Chat: chatService},
	)
}

type routerSessionStore struct {
	messages []session.Message
	summary  session.Summary
	loadErr  error
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
