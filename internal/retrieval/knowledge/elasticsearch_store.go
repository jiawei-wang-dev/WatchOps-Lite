package knowledge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	elasticsearchplatform "github.com/jiawei-wang-dev/WatchOps-Lite/internal/platform/elasticsearch"
)

const maxResponseSize = 8 << 20

type ElasticsearchStore struct {
	client elasticsearchplatform.Executor
	index  string
}

func NewElasticsearchStore(client elasticsearchplatform.Executor, index string) (*ElasticsearchStore, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: Elasticsearch client is required", ErrInvalidArgument)
	}
	index = strings.TrimSpace(index)
	if index == "" {
		return nil, fmt.Errorf("%w: Elasticsearch index is required", ErrInvalidArgument)
	}
	return &ElasticsearchStore{client: client, index: index}, nil
}

func (s *ElasticsearchStore) EnsureIndex(ctx context.Context) error {
	response, err := s.client.Do(ctx, elasticsearchplatform.Request{
		Method: http.MethodHead,
		Path:   "/" + url.PathEscape(s.index),
	})
	if err != nil {
		return dependencyError(err)
	}
	if response.StatusCode == http.StatusOK {
		response.Body.Close()
		return nil
	}
	if response.StatusCode != http.StatusNotFound {
		return responseError(response, "check knowledge index")
	}
	response.Body.Close()

	body, err := json.Marshal(map[string]any{
		"mappings": map[string]any{
			"properties": map[string]any{
				"chunk_id":    map[string]string{"type": "keyword"},
				"document_id": map[string]string{"type": "keyword"},
				"title":       map[string]string{"type": "text"},
				"content":     map[string]string{"type": "text"},
				"source":      map[string]string{"type": "keyword"},
				"chunk_index": map[string]string{"type": "integer"},
				"metadata":    map[string]any{"type": "object", "dynamic": true},
				"created_at":  map[string]string{"type": "date"},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("encode index mapping: %w", err)
	}

	response, err = s.client.Do(ctx, elasticsearchplatform.Request{
		Method:      http.MethodPut,
		Path:        "/" + url.PathEscape(s.index),
		Body:        bytes.NewReader(body),
		ContentType: "application/json",
	})
	if err != nil {
		return dependencyError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response, "create knowledge index")
	}
	return nil
}

func (s *ElasticsearchStore) IndexChunks(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	var body bytes.Buffer
	writer := bufio.NewWriter(&body)
	encoder := json.NewEncoder(writer)
	for _, chunk := range chunks {
		action := map[string]any{"index": map[string]string{
			"_index": s.index,
			"_id":    chunk.ID,
		}}
		if err := encoder.Encode(action); err != nil {
			return fmt.Errorf("encode bulk action: %w", err)
		}
		if err := encoder.Encode(chunk); err != nil {
			return fmt.Errorf("encode knowledge chunk: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush bulk request: %w", err)
	}

	response, err := s.client.Do(ctx, elasticsearchplatform.Request{
		Method:      http.MethodPost,
		Path:        "/_bulk?refresh=wait_for",
		Body:        &body,
		ContentType: "application/x-ndjson",
	})
	if err != nil {
		return dependencyError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response, "index knowledge chunks")
	}

	var result struct {
		Errors bool `json:"errors"`
	}
	if err := decodeJSON(response.Body, &result); err != nil {
		return fmt.Errorf("decode bulk response: %w", err)
	}
	if result.Errors {
		return fmt.Errorf("%w: Elasticsearch rejected one or more knowledge chunks", ErrUnavailable)
	}
	return nil
}

func (s *ElasticsearchStore) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	boolQuery := map[string]any{
		"must": []any{map[string]any{
			"multi_match": map[string]any{
				"query":  query.Query,
				"fields": []string{"title^2", "content"},
			},
		}},
	}
	if len(query.Filters) > 0 {
		filters := make([]any, 0, len(query.Filters))
		for key, value := range query.Filters {
			filters = append(filters, map[string]any{
				"term": map[string]string{"metadata." + key + ".keyword": value},
			})
		}
		boolQuery["filter"] = filters
	}

	body, err := json.Marshal(map[string]any{
		"size":  query.Limit,
		"query": map[string]any{"bool": boolQuery},
	})
	if err != nil {
		return nil, fmt.Errorf("encode search query: %w", err)
	}
	response, err := s.client.Do(ctx, elasticsearchplatform.Request{
		Method:      http.MethodPost,
		Path:        "/" + url.PathEscape(s.index) + "/_search",
		Body:        bytes.NewReader(body),
		ContentType: "application/json",
	})
	if err != nil {
		return nil, dependencyError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, responseError(response, "search knowledge")
	}

	var result struct {
		Hits struct {
			Hits []struct {
				ID     string  `json:"_id"`
				Score  float64 `json:"_score"`
				Source Chunk   `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := decodeJSON(response.Body, &result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	results := make([]SearchResult, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		chunkID := hit.Source.ID
		if chunkID == "" {
			chunkID = hit.ID
		}
		results = append(results, SearchResult{
			ChunkID:    chunkID,
			DocumentID: hit.Source.DocumentID,
			Title:      hit.Source.Title,
			Content:    hit.Source.Content,
			Source:     hit.Source.Source,
			Score:      hit.Score,
			Metadata:   cloneMetadata(hit.Source.Metadata),
		})
	}
	return results, nil
}

func (s *ElasticsearchStore) GetDocument(ctx context.Context, documentID string) (DocumentInfo, error) {
	body, err := json.Marshal(map[string]any{
		"size": 1000,
		"query": map[string]any{
			"term": map[string]string{"document_id": documentID},
		},
		"sort": []any{map[string]string{"chunk_index": "asc"}},
		"_source": []string{
			"document_id", "title", "source", "metadata", "created_at",
		},
	})
	if err != nil {
		return DocumentInfo{}, fmt.Errorf("encode document query: %w", err)
	}
	response, err := s.client.Do(ctx, elasticsearchplatform.Request{
		Method:      http.MethodPost,
		Path:        "/" + url.PathEscape(s.index) + "/_search",
		Body:        bytes.NewReader(body),
		ContentType: "application/json",
	})
	if err != nil {
		return DocumentInfo{}, dependencyError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DocumentInfo{}, responseError(response, "get knowledge document")
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source Chunk `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := decodeJSON(response.Body, &result); err != nil {
		return DocumentInfo{}, fmt.Errorf("decode document response: %w", err)
	}
	if len(result.Hits.Hits) == 0 {
		return DocumentInfo{}, ErrNotFound
	}
	first := result.Hits.Hits[0].Source
	return DocumentInfo{
		ID:         first.DocumentID,
		Title:      first.Title,
		Source:     first.Source,
		Metadata:   cloneMetadata(first.Metadata),
		CreatedAt:  first.CreatedAt,
		ChunkCount: len(result.Hits.Hits),
	}, nil
}

func decodeJSON(reader io.Reader, target any) error {
	return json.NewDecoder(io.LimitReader(reader, maxResponseSize)).Decode(target)
}

func dependencyError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func responseError(response *http.Response, operation string) error {
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = response.Status
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%w: %s: %s", ErrUnavailable, operation, message)
	}
	return fmt.Errorf("%s: Elasticsearch returned %s: %s", operation, response.Status, message)
}
