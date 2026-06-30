package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type evalExecutorStub struct {
	createResult eval.CreateResult
	cases        []eval.Case
	err          error
}

func (s evalExecutorStub) Create(context.Context, eval.Case) (eval.CreateResult, error) {
	return s.createResult, s.err
}

func (s evalExecutorStub) List(context.Context, eval.ListQuery) ([]eval.Case, error) {
	return s.cases, s.err
}

func TestEvalCreateReturnsCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	target := NewEval(evalExecutorStub{
		createResult: eval.CreateResult{CaseID: "eval_1", Status: "created"},
	})
	router := gin.New()
	router.POST("/api/v1/eval/cases", target.Create)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/eval/cases",
		bytes.NewBufferString(`{
			"feedback_id":"fb_1",
			"case_type":"bad_case",
			"input_message":"Why did checkout fail?",
			"expected_behavior":"Cite evidence."
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response dto.CreateEvalCaseResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.CaseID != "eval_1" {
		t.Fatalf("response = %#v", response)
	}
}

func TestEvalListReturnsCases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	target := NewEval(evalExecutorStub{cases: []eval.Case{{
		ID:               "eval_1",
		CaseType:         eval.CaseTypeGood,
		InputMessage:     "Question",
		ExpectedBehavior: "Cite evidence.",
	}}})
	router := gin.New()
	router.GET("/api/v1/eval/cases", target.List)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/eval/cases?case_type=good_case&limit=5", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response dto.ListEvalCasesResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Cases) != 1 || response.Cases[0].ID != "eval_1" {
		t.Fatalf("response = %#v", response)
	}
}
