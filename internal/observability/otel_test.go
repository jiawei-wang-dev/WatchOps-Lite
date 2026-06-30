package observability

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"go.opentelemetry.io/otel/trace"
)

func TestSetupTelemetryDisabledUsesNoopProvider(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := config.Default().Telemetry
	cfg.Enabled = false

	telemetry, err := SetupTelemetry(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("SetupTelemetry() error = %v", err)
	}
	ctx, span := StartSpan(context.Background(), "test.disabled")
	defer span.End()
	if trace.SpanFromContext(ctx).SpanContext().IsValid() {
		t.Fatal("disabled telemetry created a valid span context")
	}
	if err := telemetry.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestSetupTelemetryDoesNotRequireAvailableCollector(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := config.Default().Telemetry
	cfg.Enabled = true
	cfg.OTLPEndpoint = "127.0.0.1:1"
	cfg.ExportTimeout = config.Duration(20 * time.Millisecond)

	telemetry, err := SetupTelemetry(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("SetupTelemetry() error = %v, want resilient setup", err)
	}
	_, span := StartSpan(context.Background(), "test.unavailable_collector")
	span.End()

	shutdownContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = telemetry.Shutdown(shutdownContext)
}
