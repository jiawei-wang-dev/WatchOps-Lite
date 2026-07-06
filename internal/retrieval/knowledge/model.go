package knowledge

import (
	"errors"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid knowledge request")
	ErrUnavailable     = errors.New("knowledge retrieval unavailable")
	ErrNotFound        = errors.New("knowledge document not found")
)

type Document struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Source    string         `json:"source"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

type Chunk struct {
	ID          string         `json:"chunk_id"`
	DocumentID  string         `json:"document_id"`
	ContentHash string         `json:"content_hash,omitempty"`
	Title       string         `json:"title"`
	Content     string         `json:"content"`
	Source      string         `json:"source"`
	Index       int            `json:"chunk_index"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
	Embedding   []float32      `json:"embedding,omitempty"`
}

type SearchQuery struct {
	Query   string
	Limit   int
	Filters map[string]string
}

type RetrievalRequest struct {
	Query      string
	TopK       int
	VectorTopK int
	BM25TopK   int
	RerankTopK int
	Filters    map[string]string
}

type RetrievedKnowledge struct {
	ID              string         `json:"id"`
	ChunkID         string         `json:"chunk_id"`
	DocumentID      string         `json:"document_id"`
	Title           string         `json:"title"`
	Source          string         `json:"source"`
	Content         string         `json:"content"`
	Score           float64        `json:"score"`
	BM25Score       float64        `json:"bm25_score,omitempty"`
	VectorScore     float64        `json:"vector_score,omitempty"`
	FusedScore      float64        `json:"fused_score,omitempty"`
	RerankScore     float64        `json:"rerank_score,omitempty"`
	RetrievalMethod string         `json:"retrieval_method"`
	Category        string         `json:"category,omitempty"`
	Tags            []string       `json:"tags,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type RetrievalResult struct {
	Chunks   []RetrievedKnowledge `json:"chunks"`
	Metadata map[string]any       `json:"metadata"`
}

type SearchResult struct {
	ChunkID       string         `json:"chunk_id"`
	DocumentID    string         `json:"document_id"`
	ChunkIndex    int            `json:"chunk_index"`
	Title         string         `json:"title"`
	Content       string         `json:"content"`
	Source        string         `json:"source"`
	Score         float64        `json:"score"`
	Metadata      map[string]any `json:"metadata"`
	RetrievalMode string         `json:"retrieval_mode"`
	BM25Score     *float64       `json:"bm25_score,omitempty"`
	VectorScore   *float64       `json:"vector_score,omitempty"`
	RRFScore      *float64       `json:"rrf_score,omitempty"`
}

type VectorSearchQuery struct {
	Vector  []float32
	Limit   int
	Filters map[string]string
}

type DocumentInfo struct {
	ID         string         `json:"document_id"`
	Title      string         `json:"title"`
	Source     string         `json:"source"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  time.Time      `json:"created_at"`
	ChunkCount int            `json:"chunk_count"`
}

type IngestResult struct {
	DocumentID string `json:"document_id"`
	ChunkCount int    `json:"chunk_count"`
	Status     string `json:"status"`
}

type DuplicateDocument struct {
	DocumentID string
	ChunkCount int
}
