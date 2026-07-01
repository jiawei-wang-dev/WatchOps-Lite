package metrics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	retrievalmetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/metrics"
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
}

func NewSearchTool(searcher Searcher, config SearchToolConfig) *SearchTool {
	return &SearchTool{searcher: searcher, config: config}
}

func (t *SearchTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.Execute(ctx, common.ExecuteOptions{
		ToolName: Name,
		Timeout:  t.config.Timeout,
		Fallback: "enable mock metrics fallback or retry when Prometheus is available",
	}, func(ctx context.Context) (common.ToolResult, error) {
		return t.search(ctx, input)
	})
}

func (t *SearchTool) search(
	ctx context.Context,
	input Input,
) (result common.ToolResult, resultErr error) {
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
	fallbackUsed := false
	defer func() {
		span.SetAttributes(
			attribute.Int("result_count", len(result.Evidence)),
			attribute.Bool("fallback_used", fallbackUsed),
		)
		if resultErr != nil {
			errorCode := common.ErrorCodeInternal
			var toolErr *common.ToolError
			if errors.As(resultErr, &toolErr) {
				errorCode = toolErr.Code
			} else if errors.Is(resultErr, context.DeadlineExceeded) {
				errorCode = common.ErrorCodeTimeout
			}
			span.SetAttributes(attribute.String("error_code", string(errorCode)))
			observability.MarkError(span, "Metrics query failed")
		}
		span.End()
	}()

	if toolErr := validate(input); toolErr != nil {
		return common.ToolResult{}, toolErr
	}
	at, _ := time.Parse(time.RFC3339, input.TimeRange.To)

	var samples []retrievalmetrics.Sample
	var err error
	if t.searcher == nil {
		err = retrievalmetrics.ErrUnavailable
	} else {
		samples, err = t.searcher.Query(ctx, retrievalmetrics.QueryRequest{
			Service:    input.Service,
			MetricName: input.MetricName,
			Symptom:    input.Symptom,
			At:         at,
		})
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return common.ToolResult{}, err
		}
		if t.config.FallbackToMock {
			fallbackUsed = true
			result = mockResult(input)
			result.Warnings = append(result.Warnings, common.ToolWarning{
				Code:    "METRICS_FALLBACK",
				Message: "Prometheus was unavailable; mock metric evidence was returned.",
			})
			result.Metadata["mode"] = "mock_fallback"
			result.Metadata["backend"] = "mock"
			result.Metadata["configured_backend"] = backend
			result.Metadata["fallback_used"] = true
			return result, nil
		}
		return common.ToolResult{}, common.NewToolError(
			common.ErrorCodeDependencyUnavailable,
			Name,
			"Prometheus metrics backend is unavailable",
			true,
			map[string]any{"backend": "prometheus"},
			"enable metrics fallback or retry later",
		)
	}

	evidence := make([]common.EvidenceItem, 0, len(samples))
	for index, sample := range samples {
		timestamp := sample.Timestamp.UTC().Format(time.RFC3339Nano)
		service := sample.Service
		if service == "" {
			service = strings.TrimSpace(input.Service)
		}
		evidence = append(evidence, common.EvidenceItem{
			ID:         fmt.Sprintf("prometheus-%s-%d", evidenceIDPart(sample.Name), index+1),
			SourceType: "metrics",
			SourceName: "prometheus",
			TimeRange:  &input.TimeRange,
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
	warnings := []common.ToolWarning{}
	if len(evidence) == 0 {
		warnings = append(warnings, common.ToolWarning{
			Code:    "METRICS_NO_DATA",
			Message: "Prometheus returned no samples for the selected query and time.",
		})
	}
	return common.ToolResult{
		Evidence: evidence,
		Warnings: warnings,
		Payload: map[string]any{
			"returned_count": len(evidence),
		},
		Metadata: map[string]any{
			"mode":          "prometheus",
			"backend":       backend,
			"fallback_used": false,
		},
	}, nil
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
