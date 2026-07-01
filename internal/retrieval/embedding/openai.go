package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const maxEmbeddingResponseBytes = 8 << 20

type OpenAICompatibleConfig struct {
	BaseURL   string
	APIKey    string
	Model     string
	Dimension int
	Timeout   time.Duration
}

type OpenAICompatible struct {
	endpoint   string
	apiKey     string
	model      string
	dimension  int
	httpClient *http.Client
}

func NewOpenAICompatible(config OpenAICompatibleConfig) (*OpenAICompatible, error) {
	parsed, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse embedding base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" ||
		parsed.User != nil {
		return nil, errors.New("embedding base URL must be an HTTP(S) URL without credentials")
	}
	if strings.TrimSpace(config.APIKey) == "" ||
		strings.TrimSpace(config.Model) == "" ||
		config.Dimension <= 0 ||
		config.Timeout <= 0 {
		return nil, errors.New("embedding API key, model, dimension, and timeout are required")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/embeddings"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return &OpenAICompatible{
		endpoint:   parsed.String(),
		apiKey:     config.APIKey,
		model:      config.Model,
		dimension:  config.Dimension,
		httpClient: &http.Client{Timeout: config.Timeout},
	}, nil
}

func (p *OpenAICompatible) Dimension() int {
	return p.dimension
}

func (p *OpenAICompatible) Embed(
	ctx context.Context,
	inputs []string,
) ([][]float32, error) {
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	body, err := json.Marshal(map[string]any{
		"model":      p.model,
		"input":      inputs,
		"dimensions": p.dimension,
	})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := p.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("embedding provider returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxEmbeddingResponseBytes)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(payload.Data) != len(inputs) {
		return nil, errors.New("embedding provider returned an unexpected vector count")
	}
	sort.Slice(payload.Data, func(left int, right int) bool {
		return payload.Data[left].Index < payload.Data[right].Index
	})
	results := make([][]float32, len(payload.Data))
	for index, item := range payload.Data {
		if item.Index != index || len(item.Embedding) != p.dimension {
			return nil, errors.New("embedding provider returned an invalid vector")
		}
		results[index] = item.Embedding
	}
	return results, nil
}
