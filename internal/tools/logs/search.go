package logs

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	retrievallogs "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/logs"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

type Searcher interface {
	Search(context.Context, retrievallogs.SearchQuery) ([]retrievallogs.Event, error)
}

const maxEvidenceMessageRunes = 1000

type SearchToolConfig struct {
	Backend        string
	Index          string
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
		backend = "elasticsearch"
	}
	runtimeConfig := toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceLogs,
		Timeout:    config.Timeout,
		Operation: func(ctx context.Context, value any) (toolruntime.Result, error) {
			input, ok := value.(Input)
			if !ok {
				return toolruntime.Result{}, invalidArgument("invalid log tool input", nil)
			}
			return tool.search(ctx, input)
		},
	}
	if config.FallbackToMock {
		runtimeConfig.Fallback = mockOperation
		runtimeConfig.FallbackWarning = toolruntime.Warning{
			Code:    "LOGS_FALLBACK",
			Message: "Elasticsearch logs were unavailable; mock log evidence was returned.",
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
		backend = "elasticsearch"
	}
	ctx, span := observability.StartSpan(
		ctx,
		"logs.search",
		attribute.String("logs.backend", backend),
		attribute.String("logs.index", t.config.Index),
		attribute.String("service", strings.TrimSpace(input.Service)),
		attribute.Int("query_length", len(strings.Join(input.Keywords, " "))),
	)
	defer func() {
		span.SetAttributes(attribute.Int("result_count", len(result.Evidence)))
		if resultErr != nil {
			observability.MarkError(span, "Logs search failed")
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
		limit = 20
	}

	if t.searcher == nil {
		return toolruntime.Result{}, dependencyUnavailable()
	}
	events, err := t.searcher.Search(ctx, retrievallogs.SearchQuery{
		Service:  input.Service,
		From:     from,
		To:       to,
		Keywords: input.Keywords,
		Level:    input.Level,
		Limit:    limit,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return toolruntime.Result{}, err
		}
		return toolruntime.Result{}, dependencyUnavailable()
	}

	evidence := make([]toolruntime.Evidence, 0, len(events))
	for _, event := range events {
		timestamp := event.Timestamp.UTC().Format(time.RFC3339Nano)
		content, truncated := conciseMessage(event.Message)
		evidence = append(evidence, toolruntime.Evidence{
			EvidenceID: event.ID,
			SourceType: toolruntime.SourceLogs,
			Source:     "elasticsearch-logs",
			TimeRange: &toolruntime.TimeRange{
				From: timestamp,
				To:   timestamp,
			},
			TraceID:    event.TraceID,
			Content:    content,
			ResourceID: event.Service,
			Metadata: map[string]any{
				"log_id":    event.ID,
				"level":     event.Level,
				"trace_id":  event.TraceID,
				"span_id":   event.SpanID,
				"timestamp": timestamp,
				"truncated": truncated,
			},
		})
	}
	return toolruntime.Result{
		Evidence: evidence,
		Payload: map[string]any{
			"returned_count": len(evidence),
			"keywords":       input.Keywords,
		},
		Metadata: map[string]any{
			"mode":    "elasticsearch",
			"backend": backend,
			"index":   t.config.Index,
		},
	}, nil
}

func dependencyUnavailable() *toolruntime.ToolError {
	return toolruntime.NewToolError(
		toolruntime.ErrorCodeDependencyUnavailable,
		Name,
		"Elasticsearch logs backend is unavailable",
		true,
		map[string]any{"backend": "elasticsearch"},
	)
}

func conciseMessage(value string) (string, bool) {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxEvidenceMessageRunes {
		return value, false
	}
	return string(runes[:maxEvidenceMessageRunes]) + "…", true
}
