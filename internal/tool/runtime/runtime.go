package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"go.opentelemetry.io/otel/attribute"
)

const DefaultTimeout = 2 * time.Second

type Operation func(context.Context, any) (Result, error)

type Config struct {
	ToolName         string
	SourceType       SourceType
	Timeout          time.Duration
	Operation        Operation
	Fallback         Operation
	FallbackWarning  Warning
	FallbackMetadata map[string]any
}

type Runtime struct {
	config Config
}

type operationResult struct {
	result Result
	err    error
}

func New(config Config) (*Runtime, error) {
	if config.ToolName == "" {
		return nil, errors.New("tool runtime name is required")
	}
	if config.SourceType == "" {
		return nil, errors.New("tool runtime source type is required")
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	if config.Operation == nil {
		return nil, errors.New("tool runtime operation is required")
	}
	return &Runtime{config: config}, nil
}

func (r *Runtime) Name() string {
	return r.config.ToolName
}

func (r *Runtime) SourceType() SourceType {
	return r.config.SourceType
}

func (r *Runtime) Execute(ctx context.Context, input any) (result Result) {
	startedAt := time.Now().UTC()
	ctx, span := observability.StartSpan(
		ctx,
		"tool.runtime.execute",
		attribute.String("tool_name", r.config.ToolName),
		attribute.String("source_type", string(r.config.SourceType)),
		attribute.Bool("fallback_used", false),
	)
	defer func() {
		errorCode := resultErrorCode(result)
		fallbackUsed, _ := result.Metadata["fallback_used"].(bool)
		span.SetAttributes(
			attribute.String("error_code", errorCode),
			attribute.Bool("fallback_used", fallbackUsed),
			attribute.Int("evidence_count", len(result.Evidence)),
			attribute.Int64("latency_ms", result.LatencyMS),
		)
		if result.Error != nil {
			observability.MarkError(span, "tool runtime execution failed")
		}
		span.End()
		runtimemetrics.ObserveTool(
			r.config.ToolName,
			errorCodeForMetrics(result),
			result.FinishedAt.Sub(result.StartedAt),
		)
	}()
	executionContext, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	completed := make(chan operationResult, 1)
	go func() {
		result, err := r.config.Operation(executionContext, input)
		completed <- operationResult{result: result, err: err}
	}()

	select {
	case <-executionContext.Done():
		if errors.Is(executionContext.Err(), context.DeadlineExceeded) {
			fallbackUsed := r.config.Fallback != nil && ctx.Err() == nil
			_, timeoutSpan := observability.StartSpan(
				ctx,
				"tool.runtime.timeout",
				attribute.String("tool_name", r.config.ToolName),
				attribute.String("source_type", string(r.config.SourceType)),
				attribute.String("error_code", ErrorCodeTimeout),
				attribute.Bool("fallback_used", fallbackUsed),
			)
			timeoutSpan.End()
			return r.fallbackOrFailure(
				ctx,
				input,
				startedAt,
				NewToolError(
					ErrorCodeTimeout,
					r.config.ToolName,
					"tool execution exceeded its deadline",
					true,
					map[string]any{"timeout_ms": r.config.Timeout.Milliseconds()},
				),
			)
		}
		return finishFailure(
			r.config,
			startedAt,
			NewToolError(
				ErrorCodeInternal,
				r.config.ToolName,
				"tool execution was canceled",
				true,
				nil,
			),
		)
	case operation := <-completed:
		if operation.err != nil {
			toolError := normalizeError(r.config.ToolName, operation.err)
			if toolError.Code != ErrorCodeInvalidArgument {
				return r.fallbackOrFailure(ctx, input, startedAt, toolError)
			}
			return finishFailure(r.config, startedAt, toolError)
		}
		return finishSuccess(r.config, startedAt, operation.result, false)
	}
}

func (r *Runtime) fallbackOrFailure(
	ctx context.Context,
	input any,
	startedAt time.Time,
	primaryError *ToolError,
) Result {
	if r.config.Fallback == nil || ctx.Err() != nil {
		return finishFailure(r.config, startedAt, primaryError)
	}
	ctx, span := observability.StartSpan(
		ctx,
		"tool.runtime.fallback",
		attribute.String("tool_name", r.config.ToolName),
		attribute.String("source_type", string(r.config.SourceType)),
		attribute.String("error_code", primaryError.Code),
		attribute.Bool("fallback_used", true),
	)
	defer span.End()
	fallbackContext, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()
	completed := make(chan operationResult, 1)
	go func() {
		result, err := r.config.Fallback(fallbackContext, input)
		completed <- operationResult{result: result, err: err}
	}()
	var fallbackResult Result
	var err error
	select {
	case <-fallbackContext.Done():
		err = fallbackContext.Err()
		_, timeoutSpan := observability.StartSpan(
			ctx,
			"tool.runtime.timeout",
			attribute.String("tool_name", r.config.ToolName),
			attribute.String("source_type", string(r.config.SourceType)),
			attribute.String("error_code", ErrorCodeTimeout),
			attribute.Bool("fallback_used", true),
		)
		timeoutSpan.End()
	case operation := <-completed:
		fallbackResult = operation.result
		err = operation.err
	}
	if err != nil {
		observability.MarkError(span, "tool runtime fallback failed")
		fallbackError := normalizeError(r.config.ToolName, err)
		fallbackError.Details = map[string]any{
			"primary_error_code":  primaryError.Code,
			"fallback_error_code": fallbackError.Code,
		}
		return finishFailure(r.config, startedAt, fallbackError)
	}
	if fallbackResult.Metadata == nil {
		fallbackResult.Metadata = map[string]any{}
	}
	fallbackResult.Metadata["fallback_used"] = true
	fallbackResult.Metadata["primary_error_code"] = primaryError.Code
	for key, value := range r.config.FallbackMetadata {
		fallbackResult.Metadata[key] = value
	}
	if r.config.FallbackWarning.Code != "" {
		fallbackResult.Warnings = append(
			fallbackResult.Warnings,
			r.config.FallbackWarning,
		)
	}
	return finishSuccess(r.config, startedAt, fallbackResult, true)
}

func resultErrorCode(result Result) string {
	if result.Error != nil {
		return result.Error.Code
	}
	if result.Metadata != nil {
		if value, ok := result.Metadata["primary_error_code"].(string); ok {
			return value
		}
	}
	return ""
}

func errorCodeForMetrics(result Result) string {
	if result.Error != nil {
		return result.Error.Code
	}
	return ""
}

func finishSuccess(
	config Config,
	startedAt time.Time,
	result Result,
	fallbackUsed bool,
) Result {
	finishedAt := time.Now().UTC()
	result.Tool = config.ToolName
	result.SourceType = config.SourceType
	result.Error = nil
	result.StartedAt = startedAt
	result.FinishedAt = finishedAt
	result.LatencyMS = finishedAt.Sub(startedAt).Milliseconds()
	if result.Evidence == nil {
		result.Evidence = []evidence.Item{}
	}
	for index := range result.Evidence {
		result.Evidence[index] = evidence.Normalize(
			result.Evidence[index],
			config.SourceType,
		)
		if err := evidence.Validate(result.Evidence[index]); err != nil {
			return finishFailure(
				config,
				startedAt,
				NewToolError(
					ErrorCodeInternal,
					config.ToolName,
					"tool returned invalid evidence",
					false,
					map[string]any{"evidence_index": index},
				),
			)
		}
	}
	if result.Warnings == nil {
		result.Warnings = []Warning{}
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if fallbackUsed {
		result.Metadata["fallback_used"] = true
	} else if _, exists := result.Metadata["fallback_used"]; !exists {
		result.Metadata["fallback_used"] = false
	}
	return result
}

func finishFailure(config Config, startedAt time.Time, toolError *ToolError) Result {
	finishedAt := time.Now().UTC()
	return Result{
		Tool:       config.ToolName,
		SourceType: config.SourceType,
		Evidence:   []evidence.Item{},
		Warnings:   []Warning{},
		Metadata:   map[string]any{"fallback_used": false},
		Error:      toolError,
		LatencyMS:  finishedAt.Sub(startedAt).Milliseconds(),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
}

func normalizeError(toolName string, err error) *ToolError {
	var toolError *ToolError
	if errors.As(err, &toolError) {
		copy := *toolError
		if copy.Source == "" {
			copy.Source = toolName
		}
		return &copy
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewToolError(
			ErrorCodeTimeout,
			toolName,
			"tool execution exceeded its deadline",
			true,
			nil,
		)
	}
	return NewToolError(
		ErrorCodeInternal,
		toolName,
		"tool execution failed",
		false,
		nil,
	)
}

var _ Tool = (*Runtime)(nil)
