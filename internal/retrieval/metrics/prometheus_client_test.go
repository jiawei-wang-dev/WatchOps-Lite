package metrics

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPrometheusClientParsesVectorResponse(t *testing.T) {
	var receivedQuery string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v1/query" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		receivedQuery = request.URL.Query().Get("query")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
			"status":"success",
			"data":{
				"resultType":"vector",
				"result":[{
					"metric":{
						"__name__":"watchops_checkout_error_rate",
						"service":"checkout",
						"environment":"demo"
					},
					"value":[1782778200,"0.062"]
				}]
			}
		}`)),
		}, nil
	})

	client, err := NewPrometheusClient("http://prometheus.test", time.Second)
	if err != nil {
		t.Fatalf("NewPrometheusClient() error = %v", err)
	}
	client.httpClient.Transport = transport
	samples, err := client.Query(
		context.Background(),
		"watchops_checkout_error_rate",
		time.Date(2026, 6, 30, 0, 10, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if receivedQuery != "watchops_checkout_error_rate" ||
		len(samples) != 1 ||
		samples[0].Name != "watchops_checkout_error_rate" ||
		samples[0].Service != "checkout" ||
		samples[0].Value != 0.062 ||
		samples[0].Labels["environment"] != "demo" {
		t.Fatalf("samples = %#v query = %q", samples, receivedQuery)
	}
}

func TestPrometheusClientRejectsUnsupportedResult(t *testing.T) {
	client, err := NewPrometheusClient("http://prometheus.test", time.Second)
	if err != nil {
		t.Fatalf("NewPrometheusClient() error = %v", err)
	}
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"status":"success",
				"data":{"resultType":"matrix","result":[]}
			}`)),
		}, nil
	})
	if _, err := client.Query(context.Background(), "up", time.Now()); err == nil {
		t.Fatal("Query() error = nil, want unsupported result error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
