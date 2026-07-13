package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const maxResponseBytes = 1 << 20

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type HTTPClient struct {
	serverURL  string
	timeout    time.Duration
	httpClient httpDoer
}

func NewHTTPClient(serverURL string, timeout time.Duration) (*HTTPClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return nil, fmt.Errorf("%w: parse server URL: %v", ErrUnavailable, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: MCP server URL must be an HTTP(S) endpoint without credentials", ErrUnavailable)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("%w: timeout must be greater than zero", ErrUnavailable)
	}
	return &HTTPClient{
		serverURL:  parsed.String(),
		timeout:    timeout,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (c *HTTPClient) CallTool(
	ctx context.Context,
	tool string,
	args map[string]any,
) (payload map[string]any, resultErr error) {
	started := time.Now()
	ctx, span := observability.StartSpan(
		ctx,
		"mcp.call",
		attribute.String("tool", tool),
		attribute.String("mcp.server", c.serverURL),
	)
	defer func() {
		status := "ok"
		if resultErr != nil {
			status = "error"
			span.SetAttributes(attribute.String("error", resultErr.Error()))
			observability.MarkError(span, "MCP call failed")
		}
		span.SetAttributes(
			attribute.String("status", status),
			attribute.Int64("latency_ms", time.Since(started).Milliseconds()),
		)
		span.End()
	}()

	if strings.TrimSpace(tool) == "" {
		return nil, fmt.Errorf("%w: tool name is required", ErrUnavailable)
	}
	if args == nil {
		args = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": args,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode request: %v", ErrUnavailable, err)
	}
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		c.serverURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: request failed: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, ToolError{Code: response.StatusCode, Message: strings.TrimSpace(string(message))}
	}

	var envelope map[string]any
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrUnavailable, err)
	}
	if errorValue, ok := envelope["error"]; ok && errorValue != nil {
		return nil, fmt.Errorf("%w: tool error: %v", ErrUnavailable, errorValue)
	}
	if result, ok := envelope["result"].(map[string]any); ok {
		return normalizeResult(result)
	}
	return normalizeResult(envelope)
}

func normalizeResult(result map[string]any) (map[string]any, error) {
	if structured, ok := result["structuredContent"].(map[string]any); ok {
		return structured, nil
	}
	if structured, ok := result["structured_content"].(map[string]any); ok {
		return structured, nil
	}
	if content, ok := result["content"].([]any); ok {
		for _, item := range content {
			contentItem, ok := item.(map[string]any)
			if !ok || contentItem["type"] != "text" {
				continue
			}
			text, ok := contentItem["text"].(string)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(text), &parsed); err != nil {
				return nil, fmt.Errorf("%w: decode text content: %v", ErrUnavailable, err)
			}
			return parsed, nil
		}
	}
	return result, nil
}
