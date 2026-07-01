package traces

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	retrievaltraces "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/traces"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

type Searcher interface {
	Search(context.Context, retrievaltraces.Query) ([]retrievaltraces.Span, error)
}

type SearchToolConfig struct {
	Backend        string
	BaseURL        string
	DefaultService string
	DefaultLimit   int
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
		backend = "jaeger"
	}
	runtimeConfig := toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceTraces,
		Timeout:    config.Timeout,
		Operation: func(ctx context.Context, value any) (toolruntime.Result, error) {
			input, ok := value.(Input)
			if !ok {
				return toolruntime.Result{}, invalidArgument("invalid trace tool input", nil)
			}
			return tool.search(ctx, input)
		},
	}
	if config.FallbackToMock {
		runtimeConfig.Fallback = mockOperation
		runtimeConfig.FallbackWarning = toolruntime.Warning{
			Code:    "TRACES_FALLBACK",
			Message: "Jaeger was unavailable; mock trace evidence was returned.",
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
		backend = "jaeger"
	}
	if strings.TrimSpace(input.Service) == "" {
		input.Service = t.config.DefaultService
	}
	ctx, span := observability.StartSpan(
		ctx,
		"traces.query",
		attribute.String("traces.backend", backend),
		attribute.String("jaeger.base_url", t.config.BaseURL),
		attribute.String("service", strings.TrimSpace(input.Service)),
		attribute.String("operation", strings.TrimSpace(input.Operation)),
		attribute.String("trace_id", strings.TrimSpace(input.TraceID)),
	)
	defer func() {
		span.SetAttributes(attribute.Int("result_count", len(result.Evidence)))
		if resultErr != nil {
			observability.MarkError(span, "Trace query failed")
		}
		span.End()
	}()

	if toolErr := validate(input); toolErr != nil {
		return toolruntime.Result{}, toolErr
	}
	from, _ := time.Parse(time.RFC3339, input.TimeRange.From)
	to, _ := time.Parse(time.RFC3339, input.TimeRange.To)
	limit := t.config.DefaultLimit
	if limit <= 0 {
		limit = 10
	}
	if t.searcher == nil {
		return toolruntime.Result{}, dependencyUnavailable()
	}
	spans, err := t.searcher.Search(ctx, retrievaltraces.Query{
		Service:   input.Service,
		TraceID:   input.TraceID,
		Operation: input.Operation,
		From:      from,
		To:        to,
		Limit:     limit,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return toolruntime.Result{}, err
		}
		return toolruntime.Result{}, dependencyUnavailable()
	}

	evidence := make([]toolruntime.Evidence, 0, len(spans))
	for _, traceSpan := range spans {
		startTime := traceSpan.StartTime.UTC()
		endTime := startTime.Add(time.Duration(traceSpan.DurationMS * float64(time.Millisecond)))
		evidence = append(evidence, toolruntime.Evidence{
			EvidenceID: "jaeger-" + traceSpan.TraceID + "-" + traceSpan.SpanID,
			SourceType: toolruntime.SourceTraces,
			Source:     "jaeger",
			TimeRange: &toolruntime.TimeRange{
				From: startTime.Format(time.RFC3339Nano),
				To:   endTime.Format(time.RFC3339Nano),
			},
			TraceID: traceSpan.TraceID,
			Content: fmt.Sprintf(
				"Span %s in service %s took %.3fms (error=%t).",
				traceSpan.Operation,
				traceSpan.Service,
				traceSpan.DurationMS,
				traceSpan.Error,
			),
			ResourceID: traceSpan.TraceID,
			Metadata: map[string]any{
				"trace_id":    traceSpan.TraceID,
				"span_id":     traceSpan.SpanID,
				"operation":   traceSpan.Operation,
				"service":     traceSpan.Service,
				"duration_ms": traceSpan.DurationMS,
				"error":       traceSpan.Error,
				"start_time":  startTime.Format(time.RFC3339Nano),
			},
		})
	}
	warnings := []toolruntime.Warning{}
	if len(evidence) == 0 {
		warnings = append(warnings, toolruntime.Warning{
			Code:    "TRACES_NO_DATA",
			Message: "Jaeger returned no spans for the requested trace query.",
		})
	}
	return toolruntime.Result{
		Evidence: evidence,
		Warnings: warnings,
		Payload: map[string]any{
			"returned_count": len(evidence),
		},
		Metadata: map[string]any{
			"mode":    "jaeger",
			"backend": backend,
		},
	}, nil
}

func dependencyUnavailable() *toolruntime.ToolError {
	return toolruntime.NewToolError(
		toolruntime.ErrorCodeDependencyUnavailable,
		Name,
		"Jaeger trace backend is unavailable",
		true,
		map[string]any{"backend": "jaeger"},
	)
}
