package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	healthHandler := NewHealth("watchops-lite")
	router.GET("/healthz", healthHandler.Handle)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
		Time    string `json:"time"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ok" || body.Service != "watchops-lite" {
		t.Fatalf("response = %#v, want healthy watchops-lite service", body)
	}
	if _, err := time.Parse(time.RFC3339Nano, body.Time); err != nil {
		t.Fatalf("time = %q, want RFC3339 timestamp: %v", body.Time, err)
	}
}
