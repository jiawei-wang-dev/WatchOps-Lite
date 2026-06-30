package knowledge

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	elasticsearchplatform "github.com/jiawei-wang-dev/WatchOps-Lite/internal/platform/elasticsearch"
)

type executorStub struct {
	requests  []elasticsearchplatform.Request
	responses []*http.Response
	err       error
}

func (e *executorStub) Do(_ context.Context, request elasticsearchplatform.Request) (*http.Response, error) {
	if request.Body != nil {
		body, _ := io.ReadAll(request.Body)
		request.Body = bytes.NewReader(body)
	}
	e.requests = append(e.requests, request)
	if e.err != nil {
		return nil, e.err
	}
	response := e.responses[0]
	e.responses = e.responses[1:]
	return response, nil
}

func TestElasticsearchStoreCreatesIndexAndBulkIndexesChunks(t *testing.T) {
	executor := &executorStub{responses: []*http.Response{
		response(http.StatusNotFound, `{}`),
		response(http.StatusOK, `{"acknowledged":true}`),
		response(http.StatusOK, `{"errors":false}`),
	}}
	store, err := NewElasticsearchStore(executor, "watchops_knowledge")
	if err != nil {
		t.Fatalf("NewElasticsearchStore() error = %v", err)
	}

	if err := store.EnsureIndex(context.Background()); err != nil {
		t.Fatalf("EnsureIndex() error = %v", err)
	}
	if err := store.IndexChunks(context.Background(), []Chunk{{
		ID: "doc_1_chunk_0000", DocumentID: "doc_1", Title: "Runbook", Content: "Check saturation",
	}}); err != nil {
		t.Fatalf("IndexChunks() error = %v", err)
	}

	if executor.requests[0].Method != http.MethodHead ||
		executor.requests[1].Method != http.MethodPut ||
		executor.requests[2].Path != "/_bulk?refresh=wait_for" {
		t.Fatalf("requests = %#v", executor.requests)
	}
	bulkBody, _ := io.ReadAll(executor.requests[2].Body)
	if !strings.Contains(string(bulkBody), `"doc_1_chunk_0000"`) {
		t.Fatalf("bulk body = %s", bulkBody)
	}
}

func TestElasticsearchStoreMapsSearchHits(t *testing.T) {
	executor := &executorStub{responses: []*http.Response{
		response(http.StatusOK, `{
			"hits":{"hits":[{
				"_id":"doc_1_chunk_0000",
				"_score":2.5,
				"_source":{
					"chunk_id":"doc_1_chunk_0000",
					"document_id":"doc_1",
					"title":"Runbook",
					"content":"Check saturation",
					"source":"manual",
					"metadata":{"category":"runbook"}
				}
			}]}
		}`),
	}}
	store, _ := NewElasticsearchStore(executor, "watchops_knowledge")

	results, err := store.Search(context.Background(), SearchQuery{Query: "saturation", Limit: 3})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].DocumentID != "doc_1" || results[0].Score != 2.5 {
		t.Fatalf("results = %#v", results)
	}
	searchBody, _ := io.ReadAll(executor.requests[0].Body)
	if !strings.Contains(string(searchBody), `"title^2"`) {
		t.Fatalf("search body = %s, want boosted title", searchBody)
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
