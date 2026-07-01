package logs

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	retrievallogs "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/logs"
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
}

func NewSearchTool(searcher Searcher, config SearchToolConfig) *SearchTool {
	return &SearchTool{searcher: searcher, config: config}
}

func (t *SearchTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.Execute(ctx, common.ExecuteOptions{
		ToolName: Name,
		Timeout:  t.config.Timeout,
		Fallback: "enable mock log fallback or retry when Elasticsearch is available",
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
			observability.MarkError(span, "Logs search failed")
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
		limit = 20
	}

	var events []retrievallogs.Event
	var err error
	if t.searcher == nil {
		err = retrievallogs.ErrUnavailable
	} else {
		events, err = t.searcher.Search(ctx, retrievallogs.SearchQuery{
			Service:  input.Service,
			From:     from,
			To:       to,
			Keywords: input.Keywords,
			Level:    input.Level,
			Limit:    limit,
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
				Code:    "LOGS_FALLBACK",
				Message: "Elasticsearch logs were unavailable; mock log evidence was returned.",
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
			"Elasticsearch logs backend is unavailable",
			true,
			map[string]any{"backend": "elasticsearch"},
			"enable logs fallback or retry later",
		)
	}

	evidence := make([]common.EvidenceItem, 0, len(events))
	for _, event := range events {
		timestamp := event.Timestamp.UTC().Format(time.RFC3339Nano)
		content, truncated := conciseMessage(event.Message)
		evidence = append(evidence, common.EvidenceItem{
			ID:         event.ID,
			SourceType: "logs",
			SourceName: "elasticsearch-logs",
			TimeRange: &common.TimeRange{
				From: timestamp,
				To:   timestamp,
			},
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
	return common.ToolResult{
		Evidence: evidence,
		Payload: map[string]any{
			"returned_count": len(evidence),
			"keywords":       input.Keywords,
		},
		Metadata: map[string]any{
			"mode":          "elasticsearch",
			"backend":       backend,
			"index":         t.config.Index,
			"fallback_used": false,
		},
	}, nil
}

func conciseMessage(value string) (string, bool) {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxEvidenceMessageRunes {
		return value, false
	}
	return string(runes[:maxEvidenceMessageRunes]) + "…", true
}
