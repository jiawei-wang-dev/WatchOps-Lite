package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRuntimeCreatesExecuteAndFallbackSpans(t *testing.T) {
	exporter := installTraceExporter(t)
	toolRuntime, err := New(Config{
		ToolName:   "query_logs",
		SourceType: SourceLogs,
		Timeout:    time.Second,
		Operation: func(context.Context, any) (Result, error) {
			return Result{}, errors.New("backend unavailable")
		},
		Fallback: func(context.Context, any) (Result, error) {
			return Result{Evidence: []evidence.Item{{
				ID:      "log-1",
				Content: "mock log",
			}}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := toolRuntime.Execute(context.Background(), struct{}{})
	if result.Error != nil {
		t.Fatalf("Execute() result = %#v", result)
	}

	spans := spansByName(exporter.GetSpans())
	assertSpanAttributes(t, spans["tool.runtime.execute"],
		attribute.String("tool_name", "query_logs"),
		attribute.String("source_type", "logs"),
		attribute.String("error_code", ErrorCodeInternal),
		attribute.Bool("fallback_used", true),
	)
	assertSpanAttributes(t, spans["tool.runtime.fallback"],
		attribute.String("tool_name", "query_logs"),
		attribute.String("source_type", "logs"),
		attribute.String("error_code", ErrorCodeInternal),
		attribute.Bool("fallback_used", true),
	)
}

func TestRuntimeCreatesTimeoutSpan(t *testing.T) {
	exporter := installTraceExporter(t)
	toolRuntime, err := New(Config{
		ToolName:   "query_traces",
		SourceType: SourceTraces,
		Timeout:    5 * time.Millisecond,
		Operation: func(ctx context.Context, _ any) (Result, error) {
			<-ctx.Done()
			return Result{}, ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result := toolRuntime.Execute(context.Background(), struct{}{})
	if result.Error == nil || result.Error.Code != ErrorCodeTimeout {
		t.Fatalf("Execute() result = %#v, want timeout", result)
	}

	spans := spansByName(exporter.GetSpans())
	assertSpanAttributes(t, spans["tool.runtime.timeout"],
		attribute.String("tool_name", "query_traces"),
		attribute.String("source_type", "traces"),
		attribute.String("error_code", ErrorCodeTimeout),
		attribute.Bool("fallback_used", false),
	)
	assertSpanAttributes(t, spans["tool.runtime.execute"],
		attribute.String("error_code", ErrorCodeTimeout),
		attribute.Bool("fallback_used", false),
	)
}

func installTraceExporter(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(noop.NewTracerProvider())
	})
	return exporter
}

func spansByName(spans tracetest.SpanStubs) map[string]tracetest.SpanStub {
	result := make(map[string]tracetest.SpanStub, len(spans))
	for _, span := range spans {
		result[span.Name] = span
	}
	return result
}

func assertSpanAttributes(
	t *testing.T,
	span tracetest.SpanStub,
	expected ...attribute.KeyValue,
) {
	t.Helper()
	if span.Name == "" {
		t.Fatal("expected span was not recorded")
	}
	actual := make(map[attribute.Key]attribute.Value, len(span.Attributes))
	for _, item := range span.Attributes {
		actual[item.Key] = item.Value
	}
	for _, item := range expected {
		value, ok := actual[item.Key]
		if !ok || value != item.Value {
			t.Errorf("span %q attribute %q = %v, want %v", span.Name, item.Key, value, item.Value)
		}
	}
}
