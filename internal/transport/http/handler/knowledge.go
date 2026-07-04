package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type KnowledgeExecutor interface {
	Ingest(context.Context, retrievalknowledge.Document) (retrievalknowledge.IngestResult, error)
	Search(context.Context, retrievalknowledge.SearchQuery) ([]retrievalknowledge.SearchResult, error)
	GetDocument(context.Context, string) (retrievalknowledge.DocumentInfo, error)
}

type Knowledge struct {
	executor KnowledgeExecutor
}

func NewKnowledge(executor KnowledgeExecutor) *Knowledge {
	return &Knowledge{executor: executor}
}

func (h *Knowledge) Ingest(c *gin.Context) {
	var request dto.IngestKnowledgeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "request body is invalid", c.GetString("request_id"))
		return
	}
	result, err := h.executor.Ingest(c.Request.Context(), retrievalknowledge.Document{
		Title:    request.Title,
		Source:   request.Source,
		Content:  request.Content,
		Metadata: request.Metadata,
	})
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	statusCode := http.StatusCreated
	if result.Status == "skipped_duplicate" ||
		result.Status == "already_exists" {
		statusCode = http.StatusOK
	}
	c.JSON(statusCode, dto.IngestKnowledgeResponse{
		DocumentID: result.DocumentID,
		ChunkCount: result.ChunkCount,
		Status:     result.Status,
	})
}

func (h *Knowledge) Search(c *gin.Context) {
	var request dto.SearchKnowledgeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "request body is invalid", c.GetString("request_id"))
		return
	}
	results, err := h.executor.Search(c.Request.Context(), retrievalknowledge.SearchQuery{
		Query:   request.Query,
		Limit:   request.Limit,
		Filters: request.Filters,
	})
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	response := dto.SearchKnowledgeResponse{Results: make([]dto.KnowledgeSearchResult, 0, len(results))}
	for _, result := range results {
		response.Results = append(response.Results, dto.KnowledgeSearchResult{
			ChunkID:    result.ChunkID,
			DocumentID: result.DocumentID,
			Title:      result.Title,
			Content:    result.Content,
			Source:     result.Source,
			Score:      result.Score,
			Metadata:   result.Metadata,
		})
	}
	c.JSON(http.StatusOK, response)
}

func (h *Knowledge) GetDocument(c *gin.Context) {
	result, err := h.executor.GetDocument(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.KnowledgeDocumentResponse{
		DocumentID: result.ID,
		Title:      result.Title,
		Source:     result.Source,
		Metadata:   result.Metadata,
		CreatedAt:  result.CreatedAt,
		ChunkCount: result.ChunkCount,
	})
}

func writeKnowledgeError(c *gin.Context, err error) {
	requestID := c.GetString("request_id")
	switch {
	case errors.Is(err, retrievalknowledge.ErrInvalidArgument):
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), requestID)
	case errors.Is(err, retrievalknowledge.ErrNotFound):
		writeError(c, http.StatusNotFound, "NOT_FOUND", "knowledge document was not found", requestID)
	case errors.Is(err, retrievalknowledge.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "knowledge retrieval is unavailable", requestID)
	default:
		writeError(c, http.StatusInternalServerError, "INTERNAL", "knowledge request could not be completed", requestID)
	}
}
