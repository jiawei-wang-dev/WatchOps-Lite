package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	elasticsearchplatform "github.com/jiawei-wang-dev/WatchOps-Lite/internal/platform/elasticsearch"
)

type executorStub struct {
	requests  []elasticsearchplatform.Request
	responses []*http.Response
	err       error
}

func (e *executorStub) Do(
	_ context.Context,
	request elasticsearchplatform.Request,
) (*http.Response, error) {
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

func TestElasticsearchStoreCreatesLogsIndex(t *testing.T) {
	executor := &executorStub{responses: []*http.Response{
		response(http.StatusNotFound, `{}`),
		response(http.StatusOK, `{"acknowledged":true}`),
	}}
	store, err := NewElasticsearchStore(executor, "watchops_logs")
	if err != nil {
		t.Fatalf("NewElasticsearchStore() error = %v", err)
	}

	if err := store.EnsureIndex(context.Background()); err != nil {
		t.Fatalf("EnsureIndex() error = %v", err)
	}
	if len(executor.requests) != 2 ||
		executor.requests[0].Method != http.MethodHead ||
		executor.requests[1].Method != http.MethodPut {
		t.Fatalf("requests = %#v", executor.requests)
	}
	mapping, _ := io.ReadAll(executor.requests[1].Body)
	for _, expected := range []string{
		`"timestamp":{"type":"date"}`,
		`"service":{"type":"keyword"}`,
		`"message":{"type":"text"}`,
		`"trace_id":{"type":"keyword"}`,
	} {
		if !strings.Contains(string(mapping), expected) {
			t.Fatalf("mapping = %s, want %s", mapping, expected)
		}
	}
}

func TestElasticsearchStoreBuildsBoundedSearchAndMapsHits(t *testing.T) {
	executor := &executorStub{responses: []*http.Response{
		response(http.StatusOK, `{
			"hits":{"hits":[{
				"_id":"log_checkout_001",
				"_source":{
					"timestamp":"2026-06-30T00:10:00Z",
					"service":"checkout",
					"level":"error",
					"message":"upstream timeout calling payment",
					"trace_id":"trace-001",
					"span_id":"span-001",
					"attributes":{"dependency":"payment"},
					"created_at":"2026-06-30T00:10:01Z"
				}
			}]}
		}`),
	}}
	store, _ := NewElasticsearchStore(executor, "watchops_logs")
	from := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	to := from.Add(20 * time.Minute)

	events, err := store.Search(context.Background(), SearchQuery{
		Service:  "checkout",
		From:     from,
		To:       to,
		Keywords: []string{"error", "timeout"},
		Level:    "error",
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(events) != 1 ||
		events[0].ID != "log_checkout_001" ||
		events[0].TraceID != "trace-001" ||
		events[0].Attributes["dependency"] != "payment" {
		t.Fatalf("events = %#v", events)
	}
	if executor.requests[0].Path != "/watchops_logs/_search" {
		t.Fatalf("search path = %q", executor.requests[0].Path)
	}

	searchBody, _ := io.ReadAll(executor.requests[0].Body)
	var query map[string]any
	if err := json.Unmarshal(searchBody, &query); err != nil {
		t.Fatalf("decode search body: %v", err)
	}
	if query["size"] != float64(20) {
		t.Fatalf("search size = %#v, want 20", query["size"])
	}
	bodyText := string(searchBody)
	for _, expected := range []string{
		`"service":"checkout"`,
		`"level":"error"`,
		`"query":"error timeout"`,
		`"gte":"2026-06-30T00:00:00Z"`,
		`"lte":"2026-06-30T00:20:00Z"`,
		`"timestamp":"desc"`,
	} {
		if !strings.Contains(bodyText, expected) {
			t.Fatalf("search body = %s, want %s", bodyText, expected)
		}
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
