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
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/feedback"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/redisstore"
	sessionSummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
	elasticsearchplatform "github.com/jiawei-wang-dev/WatchOps-Lite/internal/platform/elasticsearch"
	mysqlplatform "github.com/jiawei-wang-dev/WatchOps-Lite/internal/platform/mysql"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	httptransport "github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http"
	"github.com/redis/go-redis/v9"
)

type App struct {
	server              *http.Server
	logger              *slog.Logger
	redisClient         *redis.Client
	elasticsearchClient *elasticsearchplatform.Client
	mysqlClient         *mysqlplatform.Client
	shutdownTimeout     time.Duration
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	var elasticsearchClient *elasticsearchplatform.Client
	knowledgeStore := retrievalknowledge.Store(retrievalknowledge.UnavailableStore{})
	if cfg.Elasticsearch.Enabled {
		client, err := elasticsearchplatform.New(elasticsearchplatform.Config{
			Addresses:      cfg.Elasticsearch.Addresses,
			Username:       cfg.Elasticsearch.Username,
			Password:       cfg.Elasticsearch.Password,
			RequestTimeout: cfg.Elasticsearch.RequestTimeout.Value(),
		})
		if err != nil {
			return nil, err
		}
		elasticsearchClient = client
		store, err := retrievalknowledge.NewElasticsearchStore(client, cfg.Elasticsearch.KnowledgeIndex)
		if err != nil {
			_ = client.Close(context.Background())
			return nil, err
		}
		knowledgeStore = store
	}
	knowledgeService, err := retrievalknowledge.NewService(knowledgeStore, cfg.Knowledge.ChunkMaxSize)
	if err != nil {
		if elasticsearchClient != nil {
			_ = elasticsearchClient.Close(context.Background())
		}
		return nil, err
	}
	if cfg.Elasticsearch.Enabled {
		indexContext, cancel := context.WithTimeout(context.Background(), cfg.Elasticsearch.RequestTimeout.Value())
		if err := knowledgeService.EnsureIndex(indexContext); err != nil {
			logger.Warn("Elasticsearch knowledge index is not ready; startup will continue", "error", err)
		}
		cancel()
	}

	toolConfig := agenteino.MockToolsConfig{}
	if cfg.Elasticsearch.Enabled {
		toolConfig.KnowledgeSearcher = knowledgeService
		toolConfig.KnowledgeTimeout = cfg.Elasticsearch.RequestTimeout.Value()
	}
	tools, err := agenteino.BuildMockToolsWithConfig(toolConfig)
	if err != nil {
		if elasticsearchClient != nil {
			_ = elasticsearchClient.Close(context.Background())
		}
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
		if elasticsearchClient != nil {
			_ = elasticsearchClient.Close(context.Background())
		}
		return nil, err
	}

	var mysqlClient *mysqlplatform.Client
	feedbackStore := feedback.Store(feedback.UnavailableStore{})
	evalStore := eval.Store(eval.UnavailableStore{})
	if cfg.MySQL.Enabled {
		client, err := mysqlplatform.New(mysqlplatform.Config{
			DSN:             cfg.MySQL.DSN,
			MaxOpenConns:    cfg.MySQL.MaxOpenConns,
			MaxIdleConns:    cfg.MySQL.MaxIdleConns,
			ConnMaxLifetime: cfg.MySQL.ConnMaxLifetime.Value(),
			RequestTimeout:  cfg.MySQL.RequestTimeout.Value(),
		})
		if err != nil {
			_ = redisClient.Close()
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
		mysqlClient = client
		mysqlFeedbackStore, err := feedback.NewMySQLStore(client.DB())
		if err != nil {
			_ = client.Close()
			_ = redisClient.Close()
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
		mysqlEvalStore, err := eval.NewMySQLStore(client.DB())
		if err != nil {
			_ = client.Close()
			_ = redisClient.Close()
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
		feedbackStore = mysqlFeedbackStore
		evalStore = mysqlEvalStore

		mysqlContext, cancel := context.WithTimeout(context.Background(), cfg.MySQL.RequestTimeout.Value())
		if err := client.Ping(mysqlContext); err != nil {
			logger.Warn("MySQL is not ready; startup will continue", "error", err)
		} else if err := client.EnsureSchema(mysqlContext); err != nil {
			logger.Warn("MySQL feedback and eval schema is not ready; startup will continue", "error", err)
		}
		cancel()
	}
	feedbackService, err := feedback.NewService(feedbackStore)
	if err != nil {
		return nil, err
	}
	evalService, err := eval.NewService(evalStore, feedbackService)
	if err != nil {
		return nil, err
	}

	agentRunner := buildAgentRunner(
		context.Background(),
		cfg,
		tools,
		logger,
		newOpenAICompatibleModel,
	)
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
		httptransport.RouterDependencies{
			Chat:      chatService,
			Knowledge: knowledgeService,
			Feedback:  feedbackService,
			Eval:      evalService,
		},
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
		logger:              logger,
		redisClient:         redisClient,
		elasticsearchClient: elasticsearchClient,
		mysqlClient:         mysqlClient,
		shutdownTimeout:     cfg.Server.ShutdownTimeout.Value(),
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	defer func() {
		if err := a.redisClient.Close(); err != nil {
			a.logger.Warn("failed to close Redis client", "error", err)
		}
		if a.elasticsearchClient != nil {
			if err := a.elasticsearchClient.Close(context.Background()); err != nil {
				a.logger.Warn("failed to close Elasticsearch client", "error", err)
			}
		}
		if a.mysqlClient != nil {
			if err := a.mysqlClient.Close(); err != nil {
				a.logger.Warn("failed to close MySQL client", "error", err)
			}
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
