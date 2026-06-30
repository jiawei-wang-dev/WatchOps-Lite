package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/bootstrap"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
)

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", configPathFromEnvironment(), "path to a JSON configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		return 1
	}

	logger := observability.NewLogger(cfg.Log.Level, cfg.Telemetry.ServiceName)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	telemetry, err := observability.SetupTelemetry(ctx, cfg.Telemetry, logger)
	if err != nil {
		logger.Error("failed to initialize OpenTelemetry", "error", err)
		return 1
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			cfg.Telemetry.ExportTimeout.Value(),
		)
		defer cancel()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			logger.Error("failed to shut down OpenTelemetry", "error", err)
		}
	}()

	app, err := bootstrap.New(cfg, logger)
	if err != nil {
		logger.Error("failed to build application", "error", err)
		return 1
	}
	if err := app.Run(ctx); err != nil {
		logger.Error("server stopped with an error", "error", err)
		return 1
	}

	return 0
}

func configPathFromEnvironment() string {
	if value := os.Getenv("WATCHOPS_CONFIG_FILE"); value != "" {
		return value
	}
	return "configs/config.json"
}
