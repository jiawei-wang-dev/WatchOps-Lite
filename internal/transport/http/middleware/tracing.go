package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const traceIDHeader = "X-Trace-ID"

func TraceRequests() gin.HandlerFunc {
	return func(c *gin.Context) {
		parentContext := otel.GetTextMapPropagator().Extract(
			c.Request.Context(),
			propagation.HeaderCarrier(c.Request.Header),
		)
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		ctx, span := otel.Tracer("github.com/jiawei-wang-dev/WatchOps-Lite/http").Start(
			parentContext,
			"HTTP "+c.Request.Method+" "+route,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", c.Request.Method),
				attribute.String("http.route", route),
				attribute.String("request_id", c.GetString("request_id")),
			),
		)
		defer span.End()

		c.Request = c.Request.WithContext(ctx)
		if traceID := observability.TraceID(ctx); traceID != "" {
			c.Header(traceIDHeader, traceID)
		}
		c.Next()

		finalRoute := c.FullPath()
		if finalRoute == "" {
			finalRoute = route
		}
		span.SetName("HTTP " + c.Request.Method + " " + finalRoute)
		span.SetAttributes(
			attribute.String("http.route", finalRoute),
			attribute.Int("http.status_code", c.Writer.Status()),
		)
		if c.Writer.Status() >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(c.Writer.Status()))
		}
	}
}
