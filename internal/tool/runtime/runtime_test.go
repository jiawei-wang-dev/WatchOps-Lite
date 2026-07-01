package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
)

func TestRuntimeExecutesOperation(t *testing.T) {
	toolRuntime, err := New(Config{
		ToolName:   "query_logs",
		SourceType: SourceLogs,
		Timeout:    time.Second,
		Operation: func(context.Context, any) (Result, error) {
			return Result{Evidence: []evidence.Item{{
				ID:      "log-1",
				Content: "log evidence",
			}}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := toolRuntime.Execute(context.Background(), struct{}{})

	if result.Error != nil || result.Tool != "query_logs" ||
		result.SourceType != SourceLogs || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v, want successful normalized result", result)
	}
	if result.Evidence[0].Source != SourceLogs {
		t.Fatalf("evidence source = %q, want %q", result.Evidence[0].Source, SourceLogs)
	}
}

func TestRuntimeUsesFallbackForOperationError(t *testing.T) {
	toolRuntime, err := New(Config{
		ToolName:   "query_metrics",
		SourceType: SourceMetrics,
		Timeout:    time.Second,
		Operation: func(context.Context, any) (Result, error) {
			return Result{}, errors.New("prometheus unavailable")
		},
		Fallback: func(context.Context, any) (Result, error) {
			return Result{Evidence: []evidence.Item{{
				ID:      "mock-metric-1",
				Content: "metric evidence",
			}}}, nil
		},
		FallbackWarning: Warning{
			Code:    "METRICS_FALLBACK",
			Message: "Mock evidence was returned.",
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := toolRuntime.Execute(context.Background(), struct{}{})

	if result.Error != nil || result.Metadata["fallback_used"] != true ||
		len(result.Warnings) != 1 || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v, want successful explicit fallback", result)
	}
	if result.Metadata["primary_error_code"] != ErrorCodeInternal {
		t.Fatalf("metadata = %#v, want primary error code", result.Metadata)
	}
}

func TestRuntimeUsesFallbackOnTimeout(t *testing.T) {
	toolRuntime, err := New(Config{
		ToolName:   "query_traces",
		SourceType: SourceTraces,
		Timeout:    5 * time.Millisecond,
		Operation: func(ctx context.Context, _ any) (Result, error) {
			<-ctx.Done()
			return Result{}, ctx.Err()
		},
		Fallback: func(context.Context, any) (Result, error) {
			return Result{Evidence: []evidence.Item{{
				ID:      "mock-trace-1",
				Content: "trace evidence",
			}}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := toolRuntime.Execute(context.Background(), struct{}{})

	if result.Error != nil || result.Metadata["fallback_used"] != true ||
		result.Metadata["primary_error_code"] != ErrorCodeTimeout {
		t.Fatalf("result = %#v, want timeout fallback", result)
	}
}

func TestRuntimeDoesNotFallbackForInvalidArguments(t *testing.T) {
	fallbackCalled := false
	toolRuntime, err := New(Config{
		ToolName:   "search_knowledge",
		SourceType: SourceKnowledge,
		Timeout:    time.Second,
		Operation: func(context.Context, any) (Result, error) {
			return Result{}, NewToolError(
				ErrorCodeInvalidArgument,
				"search_knowledge",
				"query is required",
				false,
				nil,
			)
		},
		Fallback: func(context.Context, any) (Result, error) {
			fallbackCalled = true
			return Result{}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := toolRuntime.Execute(context.Background(), struct{}{})

	if fallbackCalled || result.Error == nil ||
		result.Error.Code != ErrorCodeInvalidArgument {
		t.Fatalf("result = %#v fallbackCalled=%t", result, fallbackCalled)
	}
}

func TestRuntimeRejectsInvalidEvidence(t *testing.T) {
	toolRuntime, err := New(Config{
		ToolName:   "query_logs",
		SourceType: SourceLogs,
		Timeout:    time.Second,
		Operation: func(context.Context, any) (Result, error) {
			return Result{Evidence: []evidence.Item{{ID: "missing-content"}}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := toolRuntime.Execute(context.Background(), struct{}{})

	if result.Error == nil || result.Error.Code != ErrorCodeInternal ||
		result.Error.Message != "tool returned invalid evidence" {
		t.Fatalf("result = %#v, want invalid evidence failure", result)
	}
}
