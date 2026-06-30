package observability

import (
	"context"
	"log/slog"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
)

type Telemetry struct {
	logger *slog.Logger
}

// SetupTelemetry defines the lifecycle boundary for OpenTelemetry.
// A later task will install the SDK, resource attributes, and OTLP exporter here.
func SetupTelemetry(_ context.Context, cfg config.TelemetryConfig, logger *slog.Logger) (*Telemetry, error) {
	telemetry := &Telemetry{logger: logger}

	if !cfg.Enabled {
		logger.Info("OpenTelemetry is disabled")
		return telemetry, nil
	}

	logger.Warn(
		"OpenTelemetry exporter is not installed in the initial skeleton",
		"otlp_endpoint", cfg.OTLPEndpoint,
		"sample_ratio", cfg.SampleRatio,
	)
	return telemetry, nil
}

func (t *Telemetry) Shutdown(_ context.Context) error {
	t.logger.Debug("OpenTelemetry shutdown completed")
	return nil
}
