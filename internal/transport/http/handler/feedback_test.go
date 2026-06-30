package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/feedback"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type feedbackExecutorStub struct {
	createResult feedback.CreateResult
	value        feedback.Feedback
	err          error
}

func (s feedbackExecutorStub) Create(context.Context, feedback.Feedback) (feedback.CreateResult, error) {
	return s.createResult, s.err
}

func (s feedbackExecutorStub) Get(context.Context, string) (feedback.Feedback, error) {
	return s.value, s.err
}

func TestFeedbackCreateReturnsCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	target := NewFeedback(feedbackExecutorStub{
		createResult: feedback.CreateResult{FeedbackID: "fb_1", Status: "created"},
	})
	router := gin.New()
	router.POST("/api/v1/feedback", target.Create)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/feedback",
		bytes.NewBufferString(`{
			"request_id":"req-123",
			"session_id":"ses_01",
			"rating":"down",
			"reason_tags":["missing_evidence"]
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response dto.CreateFeedbackResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.FeedbackID != "fb_1" || response.Status != "created" {
		t.Fatalf("response = %#v", response)
	}
}

func TestFeedbackGetReportsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	target := NewFeedback(feedbackExecutorStub{err: feedback.ErrUnavailable})
	router := gin.New()
	router.GET("/api/v1/feedback/:id", target.Get)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/feedback/fb_1", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response dto.ErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("error = %#v", response.Error)
	}
}
