package logs

import (
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
	client elasticsearchplatform.Executor
	index  string
}

func NewElasticsearchStore(
	client elasticsearchplatform.Executor,
	index string,
) (*ElasticsearchStore, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: Elasticsearch client is required", ErrInvalidArgument)
	}
	index = strings.TrimSpace(index)
	if index == "" {
		return nil, fmt.Errorf("%w: Elasticsearch index is required", ErrInvalidArgument)
	}
	return &ElasticsearchStore{client: client, index: index}, nil
}

func (s *ElasticsearchStore) EnsureIndex(ctx context.Context) (resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"elasticsearch.logs.ensure_index",
		attribute.String("logs.index", s.index),
	)
	defer func() {
		if resultErr != nil {
			observability.MarkError(span, "Elasticsearch logs index operation failed")
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
		return nil
	}
	if response.StatusCode != http.StatusNotFound {
		return responseError(response, "check logs index")
	}
	response.Body.Close()

	body, err := json.Marshal(logsIndexDefinition())
	if err != nil {
		return fmt.Errorf("encode logs index mapping: %w", err)
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
		return responseError(response, "create logs index")
	}
	return nil
}

func (s *ElasticsearchStore) Search(
	ctx context.Context,
	query SearchQuery,
) (events []Event, resultErr error) {
	ctx, span := observability.StartSpan(
		ctx,
		"elasticsearch.logs.search",
		attribute.String("logs.backend", "elasticsearch"),
		attribute.String("logs.index", s.index),
		attribute.String("service", query.Service),
		attribute.Int("query_length", len(strings.Join(query.Keywords, " "))),
		attribute.Bool("fallback_used", false),
	)
	defer func() {
		span.SetAttributes(attribute.Int("result_count", len(events)))
		if resultErr != nil {
			span.SetAttributes(attribute.String("error_code", "TOOL_DEPENDENCY_UNAVAILABLE"))
			observability.MarkError(span, "Elasticsearch logs search failed")
		}
		span.End()
	}()

	body, err := buildSearchBody(query)
	if err != nil {
		return nil, fmt.Errorf("encode logs search query: %w", err)
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
		return nil, responseError(response, "search logs")
	}

	var result struct {
		Hits struct {
			Hits []struct {
				ID     string `json:"_id"`
				Source Event  `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := decodeJSON(response.Body, &result); err != nil {
		return nil, fmt.Errorf("decode logs search response: %w", err)
	}

	events = make([]Event, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		event := hit.Source
		if event.ID == "" {
			event.ID = hit.ID
		}
		if event.Attributes == nil {
			event.Attributes = map[string]any{}
		}
		events = append(events, event)
	}
	return events, nil
}

func buildSearchBody(query SearchQuery) ([]byte, error) {
	filters := []any{
		map[string]any{"term": map[string]string{"service": query.Service}},
		map[string]any{"range": map[string]any{
			"timestamp": map[string]string{
				"gte": query.From.UTC().Format(timeFormat),
				"lte": query.To.UTC().Format(timeFormat),
			},
		}},
	}
	if query.Level != "" {
		filters = append(filters, map[string]any{
			"term": map[string]string{"level": query.Level},
		})
	}

	boolQuery := map[string]any{"filter": filters}
	if len(query.Keywords) > 0 {
		boolQuery["must"] = []any{map[string]any{
			"simple_query_string": map[string]any{
				"query":            strings.Join(query.Keywords, " "),
				"fields":           []string{"message"},
				"default_operator": "or",
			},
		}}
	}
	return json.Marshal(map[string]any{
		"size":  query.Limit,
		"query": map[string]any{"bool": boolQuery},
		"sort":  []any{map[string]string{"timestamp": "desc"}},
	})
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

func logsIndexDefinition() map[string]any {
	return map[string]any{
		"mappings": map[string]any{
			"properties": map[string]any{
				"id":         map[string]string{"type": "keyword"},
				"timestamp":  map[string]string{"type": "date"},
				"service":    map[string]string{"type": "keyword"},
				"level":      map[string]string{"type": "keyword"},
				"message":    map[string]string{"type": "text"},
				"trace_id":   map[string]string{"type": "keyword"},
				"span_id":    map[string]string{"type": "keyword"},
				"attributes": map[string]any{"type": "object", "dynamic": true},
				"created_at": map[string]string{"type": "date"},
			},
		},
	}
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
	if response.StatusCode >= 500 ||
		response.StatusCode == http.StatusNotFound ||
		response.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%w: %s: %s", ErrUnavailable, operation, message)
	}
	return fmt.Errorf("%s: Elasticsearch returned %s: %s", operation, response.Status, message)
}
