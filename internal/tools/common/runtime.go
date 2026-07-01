package common

import (
	"context"
	"errors"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"go.opentelemetry.io/otel/attribute"
)

func ExecuteRuntime(
	ctx context.Context,
	toolRuntime *toolruntime.Runtime,
	input any,
) (ToolResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"tool."+toolRuntime.Name(),
		attribute.String("tool.name", toolRuntime.Name()),
	)
	result := toolRuntime.Execute(ctx, input)
	errorCode := ""
	if result.Error != nil {
		errorCode = result.Error.Code
		span.SetAttributes(attribute.String("tool.error_code", errorCode))
		observability.MarkError(span, "tool execution failed")
	}
	span.SetAttributes(
		attribute.Bool("tool.success", result.Error == nil),
		attribute.Int("tool.evidence_count", len(result.Evidence)),
		attribute.Int("tool.warning_count", len(result.Warnings)),
		attribute.Int64("tool.duration_ms", result.LatencyMS),
	)
	span.End()
	runtimemetrics.ObserveTool(
		toolRuntime.Name(),
		errorCode,
		result.FinishedAt.Sub(result.StartedAt),
	)
	return FromRuntimeResult(result)
}

func FromRuntimeResult(result toolruntime.Result) (ToolResult, error) {
	evidence := make([]EvidenceItem, 0, len(result.Evidence))
	for _, item := range result.Evidence {
		var timeRange *TimeRange
		if item.TimeRange != nil {
			timeRange = &TimeRange{
				From: item.TimeRange.From,
				To:   item.TimeRange.To,
			}
		}
		metadata := cloneMap(item.Metadata)
		if item.TraceID != "" {
			if metadata == nil {
				metadata = map[string]any{}
			}
			metadata["trace_id"] = item.TraceID
		}
		evidence = append(evidence, EvidenceItem{
			ID:         item.EvidenceID,
			SourceType: string(item.SourceType),
			SourceName: item.Source,
			TimeRange:  timeRange,
			Content:    item.Content,
			ResourceID: item.ResourceID,
			Score:      item.Score,
			Confidence: item.Confidence,
			Metadata:   metadata,
		})
	}
	warnings := make([]ToolWarning, 0, len(result.Warnings))
	for _, warning := range result.Warnings {
		warnings = append(warnings, ToolWarning{
			Code:    warning.Code,
			Message: warning.Message,
		})
	}
	commonResult := ToolResult{
		Tool:       result.Tool,
		Success:    result.Error == nil,
		Evidence:   evidence,
		Payload:    cloneMap(result.Payload),
		Warnings:   warnings,
		Metadata:   cloneMap(result.Metadata),
		StartedAt:  result.StartedAt,
		FinishedAt: result.FinishedAt,
		DurationMS: result.LatencyMS,
	}
	if result.Error == nil {
		return commonResult, nil
	}
	commonResult.Error = FromRuntimeError(result.Error)
	return commonResult, commonResult.Error
}

func ToRuntimeError(toolName string, err error) error {
	if err == nil {
		return nil
	}
	var runtimeError *toolruntime.ToolError
	if errors.As(err, &runtimeError) {
		return runtimeError
	}
	var toolError *ToolError
	if errors.As(err, &toolError) {
		return toolruntime.NewToolError(
			string(toolError.Code),
			toolName,
			toolError.Message,
			toolError.Retryable,
			cloneMap(toolError.Details),
		)
	}
	return err
}

func FromRuntimeError(runtimeError *toolruntime.ToolError) *ToolError {
	if runtimeError == nil {
		return nil
	}
	return NewToolError(
		ToolErrorCode(runtimeError.Code),
		runtimeError.Source,
		runtimeError.Message,
		runtimeError.Retryable,
		cloneMap(runtimeError.Details),
		defaultFallback(runtimeError.Code),
	)
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func defaultFallback(code string) string {
	switch code {
	case toolruntime.ErrorCodeInvalidArgument:
		return "correct the tool arguments and retry"
	case toolruntime.ErrorCodeTimeout:
		return "retry later or use a narrower time range"
	default:
		return "retry later or continue with the remaining evidence"
	}
}
