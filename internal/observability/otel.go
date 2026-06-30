package observability

import (
	"context"
	"log/slog"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type Telemetry struct {
	logger   *slog.Logger
	provider *sdktrace.TracerProvider
}

func SetupTelemetry(ctx context.Context, cfg config.TelemetryConfig, logger *slog.Logger) (*Telemetry, error) {
	telemetry := &Telemetry{logger: logger}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("OpenTelemetry export failed", "error", err)
	}))
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Enabled {
		otel.SetTracerProvider(noop.NewTracerProvider())
		logger.Info("OpenTelemetry is disabled")
		return telemetry, nil
	}

	options := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithTimeout(cfg.ExportTimeout.Value()),
	}
	if cfg.Insecure {
		options = append(options, otlptracegrpc.WithInsecure())
	}
	exporter, err := otlptracegrpc.New(ctx, options...)
	if err != nil {
		otel.SetTracerProvider(noop.NewTracerProvider())
		logger.Warn("OpenTelemetry exporter setup failed; tracing is disabled", "error", err)
		return telemetry, nil
	}

	serviceResource := resource.NewWithAttributes(
		"",
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("deployment.environment", cfg.Environment),
	)
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(serviceResource),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(provider)
	telemetry.provider = provider
	logger.Info(
		"OpenTelemetry tracing enabled",
		"otlp_endpoint", cfg.OTLPEndpoint,
		"environment", cfg.Environment,
		"sample_ratio", cfg.SampleRatio,
	)
	return telemetry, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t.provider == nil {
		return nil
	}
	err := t.provider.Shutdown(ctx)
	otel.SetTracerProvider(noop.NewTracerProvider())
	if err == nil {
		t.logger.Debug("OpenTelemetry shutdown completed")
	}
	return err
}
