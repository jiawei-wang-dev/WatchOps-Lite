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

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	elasticsearchplatform "github.com/jiawei-wang-dev/WatchOps-Lite/internal/platform/elasticsearch"
	"go.opentelemetry.io/otel/attribute"
)

const maxResponseSize = 8 << 20

type ElasticsearchStore struct {
	client          elasticsearchplatform.Executor
	index           string
	vectorDimension int
}

func NewElasticsearchStore(client elasticsearchplatform.Executor, index string) (*ElasticsearchStore, error) {
	return NewElasticsearchStoreWithVector(client, index, 0)
}

func NewElasticsearchStoreWithVector(
	client elasticsearchplatform.Executor,
	index string,
	vectorDimension int,
) (*ElasticsearchStore, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: Elasticsearch client is required", ErrInvalidArgument)
	}
	index = strings.TrimSpace(index)
	if index == "" {
		return nil, fmt.Errorf("%w: Elasticsearch index is required", ErrInvalidArgument)
	}
	if vectorDimension < 0 {
		return nil, fmt.Errorf("%w: vector dimension must not be negative", ErrInvalidArgument)
	}
	return &ElasticsearchStore{
		client:          client,
		index:           index,
		vectorDimension: vectorDimension,
	}, nil
}

func (s *ElasticsearchStore) EnsureIndex(ctx context.Context) (resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"elasticsearch.ensure_index",
		attribute.String("elasticsearch.index", s.index),
	)
	defer func() {
		if resultErr != nil {
			observability.MarkError(span, "Elasticsearch index operation failed")
		}
		span.End()
	}()

	response, err := s.client.Do(ctx, elasticsearchplatform.Request{
		Method: http.MethodHead,
		Path:   "/" + url.PathEscape(s.index),
	})
	if err != nil {
		return dependencyError(err)
	}
	if response.StatusCode == http.StatusOK {
		response.Body.Close()
		return s.ensureVectorMapping(ctx)
	}
	if response.StatusCode != http.StatusNotFound {
		return responseError(response, "check knowledge index")
	}
	response.Body.Close()

	properties := map[string]any{
		"chunk_id":     map[string]string{"type": "keyword"},
		"document_id":  map[string]string{"type": "keyword"},
		"content_hash": map[string]string{"type": "keyword"},
		"title":        map[string]string{"type": "text"},
		"content":      map[string]string{"type": "text"},
		"source":       map[string]string{"type": "keyword"},
		"chunk_index":  map[string]string{"type": "integer"},
		"metadata":     map[string]any{"type": "object", "dynamic": true},
		"created_at":   map[string]string{"type": "date"},
	}
	if s.vectorDimension > 0 {
		properties["embedding"] = s.vectorMapping()
	}
	body, err := json.Marshal(map[string]any{
		"mappings": map[string]any{
			"properties": properties,
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

func (s *ElasticsearchStore) ensureVectorMapping(ctx context.Context) error {
	properties := map[string]any{
		"content_hash": map[string]string{"type": "keyword"},
	}
	if s.vectorDimension > 0 {
		properties["embedding"] = s.vectorMapping()
	}
	body, err := json.Marshal(map[string]any{
		"properties": properties,
	})
	if err != nil {
		return fmt.Errorf("encode vector mapping: %w", err)
	}
	response, err := s.client.Do(ctx, elasticsearchplatform.Request{
		Method:      http.MethodPut,
		Path:        "/" + url.PathEscape(s.index) + "/_mapping",
		Body:        bytes.NewReader(body),
		ContentType: "application/json",
	})
	if err != nil {
		return dependencyError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response, "update knowledge vector mapping")
	}
	return nil
}

func (s *ElasticsearchStore) FindByContentHash(
	ctx context.Context,
	contentHash string,
) (DuplicateDocument, error) {
	body, err := json.Marshal(map[string]any{
		"size": 1000,
		"query": map[string]any{
			"term": map[string]string{"content_hash": contentHash},
		},
		"sort": []any{
			map[string]any{"created_at": map[string]string{
				"order":   "asc",
				"missing": "_last",
			}},
			map[string]string{"document_id": "asc"},
			map[string]string{"chunk_index": "asc"},
		},
		"_source": []string{"document_id"},
	})
	if err != nil {
		return DuplicateDocument{}, fmt.Errorf(
			"encode content hash query: %w",
			err,
		)
	}
	response, err := s.client.Do(ctx, elasticsearchplatform.Request{
		Method:      http.MethodPost,
		Path:        "/" + url.PathEscape(s.index) + "/_search",
		Body:        bytes.NewReader(body),
		ContentType: "application/json",
	})
	if err != nil {
		return DuplicateDocument{}, dependencyError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DuplicateDocument{}, responseError(
			response,
			"find knowledge content hash",
		)
	}
	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					DocumentID string `json:"document_id"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := decodeJSON(response.Body, &result); err != nil {
		return DuplicateDocument{}, fmt.Errorf(
			"decode content hash response: %w",
			err,
		)
	}
	if len(result.Hits.Hits) == 0 {
		return DuplicateDocument{}, ErrNotFound
	}
	documentID := result.Hits.Hits[0].Source.DocumentID
	chunkCount := 0
	for _, hit := range result.Hits.Hits {
		if hit.Source.DocumentID == documentID {
			chunkCount++
		}
	}
	return DuplicateDocument{
		DocumentID: documentID,
		ChunkCount: chunkCount,
	}, nil
}

func (s *ElasticsearchStore) vectorMapping() map[string]any {
	return map[string]any{
		"type":       "dense_vector",
		"dims":       s.vectorDimension,
		"index":      true,
		"similarity": "cosine",
	}
}

func (s *ElasticsearchStore) IndexChunks(ctx context.Context, chunks []Chunk) (resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"elasticsearch.bulk_index",
		attribute.String("elasticsearch.index", s.index),
		attribute.Int("chunk_count", len(chunks)),
	)
	defer func() {
		if resultErr != nil {
			observability.MarkError(span, "Elasticsearch bulk indexing failed")
		}
		span.End()
	}()

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

func (s *ElasticsearchStore) Search(
	ctx context.Context,
	query SearchQuery,
) (searchResults []SearchResult, resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"elasticsearch.search",
		attribute.String("elasticsearch.index", s.index),
		attribute.Int("query_length", len(query.Query)),
	)
	defer func() {
		span.SetAttributes(attribute.Int("result_count", len(searchResults)))
		if resultErr != nil {
			observability.MarkError(span, "Elasticsearch search failed")
		}
		span.End()
	}()

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
		"size":    query.Limit,
		"query":   map[string]any{"bool": boolQuery},
		"_source": map[string]any{"excludes": []string{"embedding"}},
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
		metadata := searchResultMetadata(hit.Source)
		results = append(results, SearchResult{
			ChunkID:    chunkID,
			DocumentID: hit.Source.DocumentID,
			ChunkIndex: hit.Source.Index,
			Title:      hit.Source.Title,
			Content:    hit.Source.Content,
			Source:     hit.Source.Source,
			Score:      hit.Score,
			Metadata:   metadata,
		})
	}
	return results, nil
}

func (s *ElasticsearchStore) SearchVector(
	ctx context.Context,
	query VectorSearchQuery,
) (searchResults []SearchResult, resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"elasticsearch.vector_search",
		attribute.String("elasticsearch.index", s.index),
		attribute.Int("vector_dimension", len(query.Vector)),
	)
	defer func() {
		span.SetAttributes(attribute.Int("result_count", len(searchResults)))
		if resultErr != nil {
			observability.MarkError(span, "Elasticsearch vector search failed")
		}
		span.End()
	}()
	if s.vectorDimension <= 0 || len(query.Vector) != s.vectorDimension {
		return nil, fmt.Errorf("%w: invalid query vector dimension", ErrInvalidArgument)
	}

	knn := map[string]any{
		"field":          "embedding",
		"query_vector":   query.Vector,
		"k":              query.Limit,
		"num_candidates": max(query.Limit*5, 100),
	}
	filters := []any{
		map[string]any{"exists": map[string]string{"field": "embedding"}},
	}
	if len(query.Filters) > 0 {
		for key, value := range query.Filters {
			filters = append(filters, map[string]any{
				"term": map[string]string{"metadata." + key + ".keyword": value},
			})
		}
	}
	knn["filter"] = map[string]any{"bool": map[string]any{"filter": filters}}
	body, err := json.Marshal(map[string]any{
		"size":    query.Limit,
		"knn":     knn,
		"_source": map[string]any{"excludes": []string{"embedding"}},
	})
	if err != nil {
		return nil, fmt.Errorf("encode vector search query: %w", err)
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
		return nil, responseError(response, "vector search knowledge")
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
		return nil, fmt.Errorf("decode vector search response: %w", err)
	}
	results := make([]SearchResult, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		chunkID := hit.Source.ID
		if chunkID == "" {
			chunkID = hit.ID
		}
		metadata := searchResultMetadata(hit.Source)
		results = append(results, SearchResult{
			ChunkID:    chunkID,
			DocumentID: hit.Source.DocumentID,
			ChunkIndex: hit.Source.Index,
			Title:      hit.Source.Title,
			Content:    hit.Source.Content,
			Source:     hit.Source.Source,
			Score:      hit.Score,
			Metadata:   metadata,
		})
	}
	return results, nil
}

func searchResultMetadata(chunk Chunk) map[string]any {
	metadata := cloneMetadata(chunk.Metadata)
	metadata["chunk_index"] = chunk.Index
	if chunk.ContentHash != "" {
		metadata["content_hash"] = chunk.ContentHash
	}
	return metadata
}

func (s *ElasticsearchStore) GetDocument(
	ctx context.Context,
	documentID string,
) (documentInfo DocumentInfo, resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"elasticsearch.get_document",
		attribute.String("elasticsearch.index", s.index),
		attribute.String("document_id", documentID),
	)
	defer func() {
		if resultErr != nil {
			observability.MarkError(span, "Elasticsearch document lookup failed")
		}
		span.End()
	}()

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
