package dto

import "time"

type IngestKnowledgeRequest struct {
	Title    string         `json:"title" binding:"required"`
	Source   string         `json:"source" binding:"required"`
	Content  string         `json:"content" binding:"required"`
	Metadata map[string]any `json:"metadata"`
}

type IngestKnowledgeResponse struct {
	DocumentID string `json:"document_id"`
	ChunkCount int    `json:"chunk_count"`
	Status     string `json:"status"`
}

type SearchKnowledgeRequest struct {
	Query   string            `json:"query" binding:"required"`
	Limit   int               `json:"limit"`
	Filters map[string]string `json:"filters"`
}

type SearchKnowledgeResponse struct {
	Results []KnowledgeSearchResult `json:"results"`
}

type KnowledgeSearchResult struct {
	ChunkID    string         `json:"chunk_id"`
	DocumentID string         `json:"document_id"`
	Title      string         `json:"title"`
	Content    string         `json:"content"`
	Source     string         `json:"source"`
	Score      float64        `json:"score"`
	Metadata   map[string]any `json:"metadata"`
}

type KnowledgeDocumentResponse struct {
	DocumentID string         `json:"document_id"`
	Title      string         `json:"title"`
	Source     string         `json:"source"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  time.Time      `json:"created_at"`
	ChunkCount int            `json:"chunk_count"`
}
