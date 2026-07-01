package traces

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	retrievaltraces "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/traces"
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
}

func NewSearchTool(searcher Searcher, config SearchToolConfig) *SearchTool {
	return &SearchTool{searcher: searcher, config: config}
}

func (t *SearchTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.Execute(ctx, common.ExecuteOptions{
		ToolName: Name,
		Timeout:  t.config.Timeout,
		Fallback: "enable mock trace fallback or retry when Jaeger is available",
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
			observability.MarkError(span, "Trace query failed")
		}
		span.End()
	}()

	if toolErr := validate(input); toolErr != nil {
		return common.ToolResult{}, toolErr
	}
	from, _ := time.Parse(time.RFC3339, input.TimeRange.From)
	to, _ := time.Parse(time.RFC3339, input.TimeRange.To)
	limit := t.config.DefaultLimit
	if limit <= 0 {
		limit = 10
	}

	var spans []retrievaltraces.Span
	var err error
	if t.searcher == nil {
		err = retrievaltraces.ErrUnavailable
	} else {
		spans, err = t.searcher.Search(ctx, retrievaltraces.Query{
			Service:   input.Service,
			TraceID:   input.TraceID,
			Operation: input.Operation,
			From:      from,
			To:        to,
			Limit:     limit,
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
				Code:    "TRACES_FALLBACK",
				Message: "Jaeger was unavailable; mock trace evidence was returned.",
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
			"Jaeger trace backend is unavailable",
			true,
			map[string]any{"backend": "jaeger"},
			"enable trace fallback or retry later",
		)
	}

	evidence := make([]common.EvidenceItem, 0, len(spans))
	for _, traceSpan := range spans {
		startTime := traceSpan.StartTime.UTC()
		endTime := startTime.Add(time.Duration(traceSpan.DurationMS * float64(time.Millisecond)))
		evidence = append(evidence, common.EvidenceItem{
			ID:         "jaeger-" + traceSpan.TraceID + "-" + traceSpan.SpanID,
			SourceType: "traces",
			SourceName: "jaeger",
			TimeRange: &common.TimeRange{
				From: startTime.Format(time.RFC3339Nano),
				To:   endTime.Format(time.RFC3339Nano),
			},
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
	warnings := []common.ToolWarning{}
	if len(evidence) == 0 {
		warnings = append(warnings, common.ToolWarning{
			Code:    "TRACES_NO_DATA",
			Message: "Jaeger returned no spans for the requested trace query.",
		})
	}
	return common.ToolResult{
		Evidence: evidence,
		Warnings: warnings,
		Payload: map[string]any{
			"returned_count": len(evidence),
		},
		Metadata: map[string]any{
			"mode":          "jaeger",
			"backend":       backend,
			"fallback_used": false,
		},
	}, nil
}
