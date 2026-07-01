package traces

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestJaegerClientBuildsSearchQueryAndParsesSpans(t *testing.T) {
	var receivedQuery string
	client, err := NewJaegerClient("http://jaeger.test", time.Second)
	if err != nil {
		t.Fatalf("NewJaegerClient() error = %v", err)
	}
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/traces" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		receivedQuery = request.URL.RawQuery
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"data":[{
					"traceID":"trace-001",
					"spans":[{
						"traceID":"trace-001",
						"spanID":"span-002",
						"operationName":"checkout.process",
						"references":[{"refType":"CHILD_OF","spanID":"span-001"}],
						"startTime":1782882846000000,
						"duration":125000,
						"tags":[{"key":"otel.status_code","type":"string","value":"ERROR"}],
						"processID":"p1"
					}],
					"processes":{"p1":{"serviceName":"watchops-lite"}}
				}]
			}`)),
		}, nil
	})

	from := time.Date(2026, 7, 1, 5, 0, 0, 0, time.UTC)
	to := from.Add(20 * time.Minute)
	spans, err := client.Search(context.Background(), Query{
		Service:   "watchops-lite",
		Operation: "checkout.process",
		From:      from,
		To:        to,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	for _, expected := range []string{
		"service=watchops-lite",
		"operation=checkout.process",
		"limit=10",
		"start=",
		"end=",
	} {
		if !strings.Contains(receivedQuery, expected) {
			t.Fatalf("query = %q, want %q", receivedQuery, expected)
		}
	}
	if len(spans) != 1 ||
		spans[0].TraceID != "trace-001" ||
		spans[0].SpanID != "span-002" ||
		spans[0].ParentSpanID != "span-001" ||
		spans[0].Service != "watchops-lite" ||
		spans[0].DurationMS != 125 ||
		!spans[0].Error {
		t.Fatalf("spans = %#v", spans)
	}
}

func TestJaegerClientUsesTraceIDEndpoint(t *testing.T) {
	client, err := NewJaegerClient("http://jaeger.test", time.Second)
	if err != nil {
		t.Fatalf("NewJaegerClient() error = %v", err)
	}
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/traces/abc123" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
		}, nil
	})

	_, err = client.Search(context.Background(), Query{
		Service: "watchops-lite",
		TraceID: "abc123",
		From:    time.Now().Add(-time.Minute),
		To:      time.Now(),
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
