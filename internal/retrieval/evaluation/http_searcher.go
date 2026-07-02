package evaluation

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
)

const maxSearchResponseBytes = 4 << 20

type HTTPSearcher struct {
	baseURL string
	client  *http.Client
}

func NewHTTPSearcher(baseURL string, timeout time.Duration) (*HTTPSearcher, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("parse API base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("API base URL must be an HTTP(S) origin")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &HTTPSearcher{
		baseURL: parsed.String(),
		client:  &http.Client{Timeout: timeout},
	}, nil
}

func (s *HTTPSearcher) Search(
	ctx context.Context,
	query string,
	limit int,
	filters map[string]string,
) ([]SearchResult, error) {
	payload, err := json.Marshal(map[string]any{
		"query":   query,
		"limit":   limit,
		"filters": filters,
	})
	if err != nil {
		return nil, fmt.Errorf("encode search request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.baseURL+"/api/v1/knowledge/search",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("knowledge API is unavailable: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxSearchResponseBytes))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("knowledge API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Results []SearchResult `json:"results"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return decoded.Results, nil
}
