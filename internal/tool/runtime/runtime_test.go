package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRuntimeExecutesOperation(t *testing.T) {
	toolRuntime, err := New(Config{
		ToolName:   "query_logs",
		SourceType: SourceLogs,
		Timeout:    time.Second,
		Operation: func(context.Context, any) (Result, error) {
			return Result{Evidence: []Evidence{{EvidenceID: "log-1"}}}, nil
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
	if result.Evidence[0].SourceType != SourceLogs {
		t.Fatalf("evidence source type = %q, want %q", result.Evidence[0].SourceType, SourceLogs)
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
			return Result{Evidence: []Evidence{{EvidenceID: "mock-metric-1"}}}, nil
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
			return Result{Evidence: []Evidence{{EvidenceID: "mock-trace-1"}}}, nil
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
