package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestTraceRequestsAddsHeaderAndPropagatesContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(noop.NewTracerProvider())
	})

	var handlerTraceID string
	router := gin.New()
	router.Use(TraceRequests())
	router.GET("/test", func(c *gin.Context) {
		spanContext := trace.SpanFromContext(c.Request.Context()).SpanContext()
		if !spanContext.IsValid() {
			t.Fatal("handler did not receive a valid span context")
		}
		handlerTraceID = spanContext.TraceID().String()
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test", nil))

	if got := recorder.Header().Get(traceIDHeader); got == "" || got != handlerTraceID {
		t.Fatalf("%s = %q, handler trace ID = %q", traceIDHeader, got, handlerTraceID)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "HTTP GET /test" {
		t.Fatalf("spans = %#v, want HTTP GET /test", spans)
	}
}
