package metrics

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Service struct {
	store   Store
	queries map[string]string
}

func NewService(store Store, queries map[string]string) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidArgument)
	}
	normalizedQueries := make(map[string]string, len(queries))
	for name, expression := range queries {
		name = strings.ToLower(strings.TrimSpace(name))
		expression = strings.TrimSpace(expression)
		if name == "" || expression == "" {
			return nil, fmt.Errorf("%w: query names and expressions are required", ErrInvalidArgument)
		}
		normalizedQueries[name] = expression
	}
	if len(normalizedQueries) == 0 {
		return nil, fmt.Errorf("%w: at least one configured query is required", ErrInvalidArgument)
	}
	return &Service{store: store, queries: normalizedQueries}, nil
}

func (s *Service) Query(ctx context.Context, request QueryRequest) ([]Sample, error) {
	request.Service = strings.TrimSpace(request.Service)
	request.MetricName = strings.ToLower(strings.TrimSpace(request.MetricName))
	request.Symptom = strings.ToLower(strings.TrimSpace(request.Symptom))
	if request.Service == "" {
		return nil, fmt.Errorf("%w: service is required", ErrInvalidArgument)
	}
	if request.MetricName == "" && request.Symptom == "" {
		return nil, fmt.Errorf("%w: metric name or symptom is required", ErrInvalidArgument)
	}
	if request.At.IsZero() {
		return nil, fmt.Errorf("%w: query timestamp is required", ErrInvalidArgument)
	}

	queryNames := s.selectQueries(request.MetricName + " " + request.Symptom)
	samples := make([]Sample, 0, len(queryNames))
	for _, queryName := range queryNames {
		expression := s.queries[queryName]
		querySamples, err := s.store.Query(ctx, expression, request.At)
		if err != nil {
			return nil, err
		}
		for index := range querySamples {
			if querySamples[index].Service != "" &&
				!strings.EqualFold(querySamples[index].Service, request.Service) {
				continue
			}
			if querySamples[index].Name == "" {
				querySamples[index].Name = queryName
			}
			if querySamples[index].Service == "" {
				querySamples[index].Service = request.Service
			}
			querySamples[index].Query = expression
			samples = append(samples, querySamples[index])
		}
	}
	return samples, nil
}

func (s *Service) selectQueries(search string) []string {
	search = strings.ToLower(strings.TrimSpace(search))
	names := make([]string, 0, len(s.queries))
	for name := range s.queries {
		names = append(names, name)
	}
	sort.Strings(names)

	selected := make([]string, 0, len(names))
	for _, name := range names {
		expression := strings.ToLower(s.queries[name])
		if search == strings.ToLower(name) || search == expression {
			return []string{name}
		}
		if matchesMetricIntent(search, name) {
			selected = append(selected, name)
		}
	}
	if len(selected) > 0 {
		return selected
	}
	return names
}

func matchesMetricIntent(search string, queryName string) bool {
	for _, keyword := range []string{"error", "latency", "timeout", "dependency"} {
		if strings.Contains(search, keyword) && strings.Contains(queryName, keyword) {
			return true
		}
	}
	return false
}
