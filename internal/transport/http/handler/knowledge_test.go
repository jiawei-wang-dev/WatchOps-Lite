package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type knowledgeExecutorStub struct {
	ingestResult retrievalknowledge.IngestResult
	results      []retrievalknowledge.SearchResult
	info         retrievalknowledge.DocumentInfo
	err          error
}

func (s knowledgeExecutorStub) Ingest(context.Context, retrievalknowledge.Document) (retrievalknowledge.IngestResult, error) {
	return s.ingestResult, s.err
}
func (s knowledgeExecutorStub) Search(context.Context, retrievalknowledge.SearchQuery) ([]retrievalknowledge.SearchResult, error) {
	return s.results, s.err
}
func (s knowledgeExecutorStub) GetDocument(context.Context, string) (retrievalknowledge.DocumentInfo, error) {
	return s.info, s.err
}

func TestKnowledgeIngestReturnsCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewKnowledge(knowledgeExecutorStub{
		ingestResult: retrievalknowledge.IngestResult{
			DocumentID: "doc_1",
			ChunkCount: 2,
			Status:     "seeded",
		},
	})
	router := gin.New()
	router.POST("/api/v1/knowledge/documents", handler.Ingest)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/knowledge/documents",
		bytes.NewBufferString(`{"title":"Runbook","source":"manual","content":"Check latency."}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response dto.IngestKnowledgeResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.DocumentID != "doc_1" ||
		response.ChunkCount != 2 ||
		response.Status != "seeded" {
		t.Fatalf("response = %#v", response)
	}
}

func TestKnowledgeIngestReturnsOKForDuplicate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewKnowledge(knowledgeExecutorStub{
		ingestResult: retrievalknowledge.IngestResult{
			DocumentID: "doc_existing",
			ChunkCount: 2,
			Status:     "skipped_duplicate",
		},
	})
	router := gin.New()
	router.POST("/api/v1/knowledge/documents", handler.Ingest)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/knowledge/documents",
		bytes.NewBufferString(
			`{"title":"Runbook","source":"manual","content":"Check latency."}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"skipped_duplicate"`)) {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestKnowledgeSearchReportsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewKnowledge(knowledgeExecutorStub{err: retrievalknowledge.ErrUnavailable})
	router := gin.New()
	router.POST("/api/v1/knowledge/search", handler.Search)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/knowledge/search",
		bytes.NewBufferString(`{"query":"latency"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

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
