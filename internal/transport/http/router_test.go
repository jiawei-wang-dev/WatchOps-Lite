package httptransport

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRouterServesHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router := NewRouter(logger, "watchops-lite")

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

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router := NewRouter(logger, "watchops-lite")
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
