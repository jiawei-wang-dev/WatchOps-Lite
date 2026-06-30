package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/redisstore"
	sessionSummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
	httptransport "github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http"
	"github.com/redis/go-redis/v9"
)

type App struct {
	server          *http.Server
	logger          *slog.Logger
	redisClient     *redis.Client
	shutdownTimeout time.Duration
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	tools, err := agenteino.BuildMockTools()
	if err != nil {
		return nil, err
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Address,
		Username:     cfg.Redis.Username,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  cfg.Redis.DialTimeout.Value(),
		ReadTimeout:  cfg.Redis.ReadTimeout.Value(),
		WriteTimeout: cfg.Redis.WriteTimeout.Value(),
	})
	sessionStore, err := redisstore.New(
		redisClient,
		cfg.Session.RecentWindowSize,
		cfg.Session.TTL.Value(),
	)
	if err != nil {
		_ = redisClient.Close()
		return nil, err
	}

	agentRunner := agenteino.NewDeterministicRunner(tools)
	chatService := applicationchat.NewService(
		agentRunner,
		sessionStore,
		sessionSummary.NewDeterministic(),
		applicationchat.ServiceConfig{
			RecentWindowSize: cfg.Session.RecentWindowSize,
			SummaryThreshold: cfg.Session.SummaryThreshold,
		},
	)
	router := httptransport.NewRouter(
		logger,
		cfg.Telemetry.ServiceName,
		httptransport.RouterDependencies{Chat: chatService},
	)

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
		redisClient:     redisClient,
		shutdownTimeout: cfg.Server.ShutdownTimeout.Value(),
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	defer func() {
		if err := a.redisClient.Close(); err != nil {
			a.logger.Warn("failed to close Redis client", "error", err)
		}
	}()

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
