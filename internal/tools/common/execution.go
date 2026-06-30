package common

import (
	"context"
	"errors"
	"time"
)

const DefaultTimeout = 2 * time.Second

type ExecuteOptions struct {
	ToolName string
	Timeout  time.Duration
	Fallback string
}

type operationResult struct {
	result ToolResult
	err    error
}

func Execute(
	ctx context.Context,
	options ExecuteOptions,
	operation func(context.Context) (ToolResult, error),
) (ToolResult, error) {
	startedAt := time.Now().UTC()
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	executionContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	completed := make(chan operationResult, 1)
	go func() {
		result, err := operation(executionContext)
		completed <- operationResult{result: result, err: err}
	}()

	select {
	case <-executionContext.Done():
		if errors.Is(executionContext.Err(), context.DeadlineExceeded) {
			toolErr := NewToolError(
				ErrorCodeTimeout,
				options.ToolName,
				"tool execution exceeded its deadline",
				true,
				map[string]any{"timeout_ms": timeout.Milliseconds()},
				fallbackOrDefault(options.Fallback, "retry later or use a narrower time range"),
			)
			return failureResult(options.ToolName, startedAt, toolErr), toolErr
		}

		toolErr := NewToolError(
			ErrorCodeInternal,
			options.ToolName,
			"tool execution was canceled",
			true,
			nil,
			fallbackOrDefault(options.Fallback, "retry the request"),
		)
		return failureResult(options.ToolName, startedAt, toolErr), toolErr

	case completedOperation := <-completed:
		if completedOperation.err != nil {
			toolErr := normalizeError(options.ToolName, completedOperation.err, options.Fallback, timeout)
			return failureResult(options.ToolName, startedAt, toolErr), toolErr
		}

		result := completedOperation.result
		finishResult(&result, options.ToolName, startedAt)
		return result, nil
	}
}

func finishResult(result *ToolResult, toolName string, startedAt time.Time) {
	finishedAt := time.Now().UTC()

	result.Tool = toolName
	result.Success = true
	result.Error = nil
	result.StartedAt = startedAt
	result.FinishedAt = finishedAt
	result.DurationMS = finishedAt.Sub(startedAt).Milliseconds()

	if result.Evidence == nil {
		result.Evidence = []EvidenceItem{}
	}
	if result.Warnings == nil {
		result.Warnings = []ToolWarning{}
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
}

func failureResult(toolName string, startedAt time.Time, toolErr *ToolError) ToolResult {
	finishedAt := time.Now().UTC()
	return ToolResult{
		Tool:       toolName,
		Success:    false,
		Evidence:   []EvidenceItem{},
		Warnings:   []ToolWarning{},
		Metadata:   map[string]any{},
		Error:      toolErr,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		DurationMS: finishedAt.Sub(startedAt).Milliseconds(),
	}
}

func normalizeError(toolName string, err error, fallback string, timeout time.Duration) *ToolError {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		copy := *toolErr
		if copy.Tool == "" {
			copy.Tool = toolName
		}
		if copy.Fallback == "" {
			copy.Fallback = fallback
		}
		return &copy
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return NewToolError(
			ErrorCodeTimeout,
			toolName,
			"tool execution exceeded its deadline",
			true,
			map[string]any{"timeout_ms": timeout.Milliseconds()},
			fallbackOrDefault(fallback, "retry later or use a narrower time range"),
		)
	}

	return NewToolError(
		ErrorCodeInternal,
		toolName,
		"tool execution failed",
		false,
		nil,
		fallbackOrDefault(fallback, "retry later"),
	)
}

func fallbackOrDefault(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
