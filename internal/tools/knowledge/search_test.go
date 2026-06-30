package knowledge

import (
	"context"
	"errors"
	"testing"

	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
)

type searcherStub struct {
	results []retrievalknowledge.SearchResult
	err     error
}

func (s searcherStub) Search(context.Context, retrievalknowledge.SearchQuery) ([]retrievalknowledge.SearchResult, error) {
	return s.results, s.err
}

func TestSearchToolReturnsElasticsearchEvidence(t *testing.T) {
	tool := NewSearchTool(searcherStub{results: []retrievalknowledge.SearchResult{{
		ChunkID: "chunk_1", DocumentID: "doc_1", Title: "Runbook",
		Content: "Inspect saturation.", Source: "manual", Score: 3.2,
	}}}, 0)

	result, err := tool.Execute(context.Background(), Input{Query: "latency", TopK: 3})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Metadata["mode"] != "elasticsearch" || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchToolFallsBackToMockEvidence(t *testing.T) {
	tool := NewSearchTool(searcherStub{err: errors.New("connection refused")}, 0)

	result, err := tool.Execute(context.Background(), Input{Query: "latency", TopK: 3})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Metadata["mode"] != "mock_fallback" || len(result.Warnings) != 1 {
		t.Fatalf("result = %#v, want explicit fallback", result)
	}
}
