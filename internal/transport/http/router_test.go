package httptransport

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
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

	assertToolRuns(t, response.ToolRuns, "query_metrics", "query_logs")
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

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()

	tools, err := agenteino.BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}
	runner := agenteino.NewDeterministicRunner(tools)
	chatService := applicationchat.NewService(runner)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	return NewRouter(
		logger,
		"watchops-lite",
		RouterDependencies{Chat: chatService},
	)
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
