package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const (
	externalProvider        = "external"
	maxExternalResponseSize = 4 << 20
)

type ExternalConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

type ExternalReranker struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

func NewExternal(config ExternalConfig) (*ExternalReranker, error) {
	parsed, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("%w: parse external rerank URL", ErrInvalidArgument)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" ||
		parsed.User != nil {
		return nil, fmt.Errorf("%w: rerank base URL must be HTTP(S) without credentials", ErrInvalidArgument)
	}
	if strings.TrimSpace(config.APIKey) == "" ||
		strings.TrimSpace(config.Model) == "" ||
		config.Timeout <= 0 {
		return nil, fmt.Errorf("%w: rerank API key, model, and timeout are required", ErrInvalidArgument)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/rerank"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return &ExternalReranker{
		endpoint: parsed.String(),
		apiKey:   strings.TrimSpace(config.APIKey),
		model:    strings.TrimSpace(config.Model),
		client:   &http.Client{Timeout: config.Timeout},
	}, nil
}

func (r *ExternalReranker) Rerank(
	ctx context.Context,
	query string,
	candidates []Candidate,
	topK int,
) ([]Result, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"retrieval.rerank.external",
		attribute.Int("candidate_count", len(candidates)),
		attribute.Int("top_k", topK),
		attribute.String("provider", externalProvider),
	)
	defer span.End()

	query = strings.TrimSpace(query)
	if query == "" {
		observability.MarkError(span, "external rerank validation failed")
		return nil, fmt.Errorf("%w: query is required", ErrInvalidArgument)
	}
	limit, err := boundedTopK(topK, len(candidates))
	if err != nil {
		observability.MarkError(span, "external rerank validation failed")
		return nil, err
	}
	if len(candidates) == 0 {
		return []Result{}, nil
	}
	documents := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		documents = append(documents, strings.TrimSpace(candidate.Title+"\n"+candidate.Content))
	}
	body, err := json.Marshal(map[string]any{
		"model":     r.model,
		"query":     query,
		"documents": documents,
		"top_n":     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode request", ErrUnavailable)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build request", ErrUnavailable)
	}
	request.Header.Set("Authorization", "Bearer "+r.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		observability.MarkError(span, "external rerank request failed")
		return nil, fmt.Errorf("%w: request failed: %w", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		observability.MarkError(span, "external rerank provider rejected request")
		return nil, fmt.Errorf("%w: provider returned HTTP %d", ErrUnavailable, response.StatusCode)
	}
	var payload struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxExternalResponseSize)).Decode(&payload); err != nil {
		observability.MarkError(span, "external rerank response decode failed")
		return nil, fmt.Errorf("%w: decode response", ErrInvalidResponse)
	}
	if len(payload.Results) == 0 {
		return nil, ErrEmptyResult
	}
	if len(payload.Results) > limit {
		return nil, fmt.Errorf("%w: provider returned too many results", ErrInvalidResponse)
	}
	seen := make(map[int]struct{}, len(payload.Results))
	results := make([]Result, 0, len(payload.Results))
	for _, item := range payload.Results {
		if item.Index < 0 || item.Index >= len(candidates) ||
			math.IsNaN(item.RelevanceScore) ||
			math.IsInf(item.RelevanceScore, 0) {
			return nil, fmt.Errorf("%w: provider returned invalid index or score", ErrInvalidResponse)
		}
		if _, exists := seen[item.Index]; exists {
			return nil, fmt.Errorf("%w: provider returned duplicate index", ErrInvalidResponse)
		}
		seen[item.Index] = struct{}{}
		results = append(results, Result{
			Candidate: candidates[item.Index],
			Score:     item.RelevanceScore,
			Reason:    "external_relevance_score",
			Provider:  externalProvider,
		})
	}
	sort.SliceStable(results, func(left, right int) bool {
		return results[left].Score > results[right].Score
	})
	return results, nil
}

var _ Reranker = (*ExternalReranker)(nil)
