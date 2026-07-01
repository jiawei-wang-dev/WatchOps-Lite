package metrics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	retrievalmetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/metrics"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

type Searcher interface {
	Query(context.Context, retrievalmetrics.QueryRequest) ([]retrievalmetrics.Sample, error)
}

type SearchToolConfig struct {
	Backend        string
	BaseURL        string
	FallbackToMock bool
	Timeout        time.Duration
}

type SearchTool struct {
	searcher Searcher
	config   SearchToolConfig
	runtime  *toolruntime.Runtime
}

func NewSearchTool(searcher Searcher, config SearchToolConfig) *SearchTool {
	tool := &SearchTool{searcher: searcher, config: config}
	backend := strings.ToLower(strings.TrimSpace(config.Backend))
	if backend == "" {
		backend = "prometheus"
	}
	runtimeConfig := toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceMetrics,
		Timeout:    config.Timeout,
		Operation: func(ctx context.Context, value any) (toolruntime.Result, error) {
			input, ok := value.(Input)
			if !ok {
				return toolruntime.Result{}, invalidArgument("invalid metrics tool input", nil)
			}
			return tool.search(ctx, input)
		},
	}
	if config.FallbackToMock {
		runtimeConfig.Fallback = mockOperation
		runtimeConfig.FallbackWarning = toolruntime.Warning{
			Code:    "METRICS_FALLBACK",
			Message: "Prometheus was unavailable; mock metric evidence was returned.",
		}
		runtimeConfig.FallbackMetadata = map[string]any{
			"mode":               "mock_fallback",
			"backend":            "mock",
			"configured_backend": backend,
		}
	}
	tool.runtime = mustRuntime(runtimeConfig)
	return tool
}

func (t *SearchTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.ExecuteRuntime(ctx, t.runtime, input)
}

func (t *SearchTool) search(
	ctx context.Context,
	input Input,
) (result toolruntime.Result, resultErr error) {
	backend := strings.ToLower(strings.TrimSpace(t.config.Backend))
	if backend == "" {
		backend = "prometheus"
	}
	queryName := strings.TrimSpace(input.MetricName)
	if queryName == "" {
		queryName = strings.TrimSpace(input.Symptom)
	}
	ctx, span := observability.StartSpan(
		ctx,
		"metrics.query",
		attribute.String("metrics.backend", backend),
		attribute.String("prometheus.base_url", t.config.BaseURL),
		attribute.String("query_name", queryName),
		attribute.Int("query_length", len(queryName)),
	)
	defer func() {
		span.SetAttributes(attribute.Int("result_count", len(result.Evidence)))
		if resultErr != nil {
			observability.MarkError(span, "Metrics query failed")
		}
		span.End()
	}()

	if toolErr := validate(input); toolErr != nil {
		return toolruntime.Result{}, toolErr
	}
	at, _ := time.Parse(time.RFC3339, input.TimeRange.To)
	if t.searcher == nil {
		return toolruntime.Result{}, dependencyUnavailable()
	}
	samples, err := t.searcher.Query(ctx, retrievalmetrics.QueryRequest{
		Service:    input.Service,
		MetricName: input.MetricName,
		Symptom:    input.Symptom,
		At:         at,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return toolruntime.Result{}, err
		}
		return toolruntime.Result{}, dependencyUnavailable()
	}

	evidenceItems := make([]evidence.Item, 0, len(samples))
	for index, sample := range samples {
		timestamp := sample.Timestamp.UTC().Format(time.RFC3339Nano)
		service := sample.Service
		if service == "" {
			service = strings.TrimSpace(input.Service)
		}
		evidenceItems = append(evidenceItems, evidence.Item{
			ID:         fmt.Sprintf("prometheus-%s-%d", evidenceIDPart(sample.Name), index+1),
			Type:       evidence.TypeMetricSample,
			Source:     evidence.SourceMetrics,
			SourceName: "prometheus",
			TimeRange: &evidence.TimeRange{
				From: input.TimeRange.From,
				To:   input.TimeRange.To,
			},
			Content: fmt.Sprintf(
				"%s=%s for service %s at %s.",
				sample.Name,
				formatMetricValue(sample.Value),
				service,
				timestamp,
			),
			ResourceID: service,
			Metadata: map[string]any{
				"metric_name": sample.Name,
				"value":       sample.Value,
				"labels":      sample.Labels,
				"query":       sample.Query,
				"timestamp":   timestamp,
			},
		})
	}
	warnings := []toolruntime.Warning{}
	if len(evidenceItems) == 0 {
		warnings = append(warnings, toolruntime.Warning{
			Code:    "METRICS_NO_DATA",
			Message: "Prometheus returned no samples for the selected query and time.",
		})
	}
	return toolruntime.Result{
		Evidence: evidenceItems,
		Warnings: warnings,
		Payload: map[string]any{
			"returned_count": len(evidenceItems),
		},
		Metadata: map[string]any{
			"mode":    "prometheus",
			"backend": backend,
		},
	}, nil
}

func dependencyUnavailable() *toolruntime.ToolError {
	return toolruntime.NewToolError(
		toolruntime.ErrorCodeDependencyUnavailable,
		Name,
		"Prometheus metrics backend is unavailable",
		true,
		map[string]any{"backend": "prometheus"},
	)
}

func formatMetricValue(value float64) string {
	return fmt.Sprintf("%.6g", value)
}

func evidenceIDPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' ||
			character == '-' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "metric"
	}
	return builder.String()
}
