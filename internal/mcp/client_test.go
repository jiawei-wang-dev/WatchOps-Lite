package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientCallToolSuccess(t *testing.T) {
	client := testHTTPClient(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s", request.Method)
		}
		return jsonResponse(http.StatusOK, `{
			"jsonrpc":"2.0",
			"id":1,
			"result":{"structuredContent":{"samples":[{"value":0.062}]}}
		}`), nil
	})

	result, err := client.CallTool(context.Background(), "query_prometheus", map[string]any{
		"query": "watchops_checkout_error_rate",
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if _, ok := result["samples"].([]any); !ok {
		t.Fatalf("result = %#v", result)
	}
}

func TestHTTPClientCallToolServerError(t *testing.T) {
	client := testHTTPClient(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadGateway, `mcp unavailable`), nil
	})

	_, err := client.CallTool(context.Background(), "query_prometheus", nil)
	var toolErr ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != http.StatusBadGateway {
		t.Fatalf("error = %v, want ToolError 502", err)
	}
}

func TestHTTPClientCallToolTimeout(t *testing.T) {
	client := testHTTPClient(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client.timeout = time.Nanosecond

	_, err := client.CallTool(context.Background(), "query_prometheus", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestDisabledClient(t *testing.T) {
	_, err := DisabledClient{}.CallTool(context.Background(), "query_prometheus", nil)
	if !errors.Is(err, ErrDisabled) || !IsUnavailable(err) {
		t.Fatalf("error = %v, want ErrDisabled", err)
	}
}

func TestHTTPClientAcceptsTextContentJSON(t *testing.T) {
	client := testHTTPClient(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"result":{
				"content":[{"type":"text","text":"{\"samples\":[{\"value\":0.062}]}"}]
			}
		}`), nil
	})

	result, err := client.CallTool(context.Background(), "query_prometheus", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if _, ok := result["samples"].([]any); !ok {
		t.Fatalf("result = %#v", result)
	}
}

func testHTTPClient(fn func(*http.Request) (*http.Response, error)) *HTTPClient {
	return &HTTPClient{
		serverURL:  "http://mcp.test",
		timeout:    time.Second,
		httpClient: roundTripDoer{roundTripFunc: fn},
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type roundTripDoer struct {
	roundTripFunc func(*http.Request) (*http.Response, error)
}

func (d roundTripDoer) Do(request *http.Request) (*http.Response, error) {
	return d.roundTripFunc(request)
}
