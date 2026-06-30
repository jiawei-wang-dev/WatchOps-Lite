package common

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecuteReturnsStructuredTimeout(t *testing.T) {
	result, err := Execute(
		context.Background(),
		ExecuteOptions{
			ToolName: "slow_tool",
			Timeout:  10 * time.Millisecond,
			Fallback: "use cached evidence",
		},
		func(ctx context.Context) (ToolResult, error) {
			<-ctx.Done()
			return ToolResult{}, ctx.Err()
		},
	)

	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("error = %v, want *ToolError", err)
	}
	if toolErr.Code != ErrorCodeTimeout {
		t.Fatalf("error code = %q, want %q", toolErr.Code, ErrorCodeTimeout)
	}
	if toolErr.Tool != "slow_tool" || !toolErr.Retryable || toolErr.Fallback == "" {
		t.Fatalf("ToolError fields not populated: %#v", toolErr)
	}
	if result.Success || result.Error == nil || result.Error.Code != ErrorCodeTimeout {
		t.Fatalf("result = %#v, want structured timeout failure", result)
	}
	if result.StartedAt.IsZero() || result.FinishedAt.IsZero() {
		t.Fatalf("result timestamps must be populated: %#v", result)
	}
}

func TestExecuteSanitizesUnexpectedErrors(t *testing.T) {
	_, err := Execute(
		context.Background(),
		ExecuteOptions{ToolName: "unsafe_tool"},
		func(context.Context) (ToolResult, error) {
			return ToolResult{}, errors.New("backend password=secret")
		},
	)

	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("error = %v, want *ToolError", err)
	}
	if toolErr.Code != ErrorCodeInternal || toolErr.Message != "tool execution failed" {
		t.Fatalf("error = %#v, want sanitized internal error", toolErr)
	}
}
