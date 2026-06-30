package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	httptransport "github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http"
)

type App struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

func New(cfg config.Config, logger *slog.Logger) *App {
	router := httptransport.NewRouter(logger, cfg.Telemetry.ServiceName)

	return &App{
		server: &http.Server{
			Addr:              cfg.Server.Address,
			Handler:           router,
			ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.Value(),
			ReadTimeout:       cfg.Server.ReadTimeout.Value(),
			WriteTimeout:      cfg.Server.WriteTimeout.Value(),
			IdleTimeout:       cfg.Server.IdleTimeout.Value(),
		},
		logger:          logger,
		shutdownTimeout: cfg.Server.ShutdownTimeout.Value(),
	}
}

func (a *App) Run(ctx context.Context) error {
	serverError := make(chan error, 1)
	go func() {
		a.logger.Info("HTTP server starting", "address", a.server.Addr)
		serverError <- a.server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		a.logger.Info("HTTP server shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	err := <-serverError
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	a.logger.Info("HTTP server stopped")
	return nil
}
