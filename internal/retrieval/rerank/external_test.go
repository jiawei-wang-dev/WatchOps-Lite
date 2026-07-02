package rerank

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestExternalRerankerUsesBoundedProviderResponse(t *testing.T) {
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1/rerank" {
			t.Errorf("path = %q, want /v1/rerank", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		var body struct {
			Model     string   `json:"model"`
			Query     string   `json:"query"`
			Documents []string `json:"documents"`
			TopN      int      `json:"top_n"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Model != "rerank-test" || body.Query != "checkout" ||
			len(body.Documents) != 2 || body.TopN != 2 {
			t.Errorf("body = %#v", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"results":[
					{"index":1,"relevance_score":0.95},
					{"index":0,"relevance_score":0.40}
				]
			}`)),
		}, nil
	})

	reranker, err := NewExternal(ExternalConfig{
		BaseURL: "https://rerank.test/v1",
		APIKey:  "test-key",
		Model:   "rerank-test",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewExternal() error = %v", err)
	}
	reranker.client.Transport = transport
	results, err := reranker.Rerank(
		context.Background(),
		"checkout",
		[]Candidate{
			{ID: "first", Title: "First", Content: "content"},
			{ID: "second", Title: "Second", Content: "content"},
		},
		2,
	)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if len(results) != 2 ||
		results[0].Candidate.ID != "second" ||
		results[0].Score != 0.95 ||
		results[0].Provider != externalProvider {
		t.Fatalf("results = %#v", results)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
