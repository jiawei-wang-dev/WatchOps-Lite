package observability

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/jiawei-wang-dev/WatchOps-Lite"

func StartSpan(
	ctx context.Context,
	name string,
	attributes ...attribute.KeyValue,
) (context.Context, trace.Span) {
	return otel.Tracer(instrumentationName).Start(
		ctx,
		name,
		trace.WithAttributes(attributes...),
	)
}

func MarkError(span trace.Span, message string) {
	span.RecordError(errors.New(message))
	span.SetStatus(codes.Error, message)
}

func TraceID(ctx context.Context) string {
	spanContext := trace.SpanFromContext(ctx).SpanContext()
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.TraceID().String()
}
