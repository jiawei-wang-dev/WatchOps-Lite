package rerank

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid rerank request")
	ErrUnavailable     = errors.New("rerank provider unavailable")
	ErrInvalidResponse = errors.New("invalid rerank response")
	ErrEmptyResult     = errors.New("rerank returned no results")
)

type Candidate struct {
	ID          string
	DocumentID  string
	ChunkID     string
	Title       string
	Content     string
	Service     string
	Source      string
	SourceType  string
	Score       float64
	BM25Score   float64
	VectorScore float64
	HybridScore float64
	Metadata    map[string]any
	CreatedAt   time.Time
}

type Result struct {
	Candidate      Candidate
	Score          float64
	Reason         string
	Provider       string
	FallbackReason string
}

type Reranker interface {
	Rerank(context.Context, string, []Candidate, int) ([]Result, error)
}

func boundedTopK(topK, candidateCount int) (int, error) {
	if topK <= 0 {
		return 0, ErrInvalidArgument
	}
	if topK > candidateCount {
		return candidateCount, nil
	}
	return topK, nil
}
