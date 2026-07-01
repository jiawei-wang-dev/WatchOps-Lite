package common

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
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

func TestExecuteRecordsToolMetrics(t *testing.T) {
	collector := runtimemetrics.New()
	runtimemetrics.SetDefault(collector)
	t.Cleanup(func() { runtimemetrics.SetDefault(nil) })

	_, err := Execute(
		context.Background(),
		ExecuteOptions{ToolName: "query_logs"},
		func(context.Context) (ToolResult, error) {
			return ToolResult{}, nil
		},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	collector.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest("GET", "/metrics", nil),
	)
	if !strings.Contains(
		recorder.Body.String(),
		`watchops_tool_calls_total{status="success",tool="query_logs"} 1`,
	) {
		t.Fatalf("tool metric was not recorded: %s", recorder.Body.String())
	}
}
