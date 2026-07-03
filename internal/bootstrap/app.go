package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/feedback"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/redisstore"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/multiagent"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	elasticsearchplatform "github.com/jiawei-wang-dev/WatchOps-Lite/internal/platform/elasticsearch"
	mysqlplatform "github.com/jiawei-wang-dev/WatchOps-Lite/internal/platform/mysql"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/profile"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	retrievallogs "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/logs"
	retrievalmetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/metrics"
	retrievaltraces "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/traces"
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
	var metricsHandler http.Handler
	runtimemetrics.SetDefault(nil)
	if cfg.RuntimeMetrics.Enabled {
		collector := runtimemetrics.New()
		runtimemetrics.SetDefault(collector)
		metricsHandler = collector.Handler()
	}
	embeddingProvider := buildEmbeddingProvider(cfg, logger)
	reranker := buildReranker(cfg, logger)
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
		vectorDimension := 0
		if embeddingProvider != nil {
			vectorDimension = embeddingProvider.Dimension()
		}
		store, err := retrievalknowledge.NewElasticsearchStoreWithVector(
			client,
			cfg.Elasticsearch.KnowledgeIndex,
			vectorDimension,
		)
		if err != nil {
			_ = client.Close(context.Background())
			return nil, err
		}
		knowledgeStore = store
	}
	knowledgeService, err := retrievalknowledge.NewServiceWithReranker(
		knowledgeStore,
		embeddingProvider,
		reranker,
		retrievalknowledge.ServiceConfig{
			ChunkMaxSize:     cfg.Knowledge.ChunkMaxSize,
			RetrievalMode:    cfg.Knowledge.RetrievalMode,
			BM25TopK:         cfg.Knowledge.BM25TopK,
			VectorTopK:       cfg.Knowledge.VectorTopK,
			FinalTopK:        cfg.Knowledge.FinalTopK,
			RRFK:             cfg.Knowledge.RRFK,
			FallbackToBM25:   cfg.Knowledge.FallbackToBM25,
			RerankCandidateK: cfg.Rerank.CandidateK,
			RerankTopK:       cfg.Rerank.TopK,
		},
	)
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

	var logsSearcher *retrievallogs.Service
	if strings.EqualFold(cfg.Logs.Backend, "elasticsearch") && elasticsearchClient != nil {
		logsStore, err := retrievallogs.NewElasticsearchStore(
			elasticsearchClient,
			cfg.Logs.Index,
		)
		if err != nil {
			_ = elasticsearchClient.Close(context.Background())
			return nil, err
		}
		logsSearcher, err = retrievallogs.NewService(logsStore)
		if err != nil {
			_ = elasticsearchClient.Close(context.Background())
			return nil, err
		}
		indexContext, cancel := context.WithTimeout(
			context.Background(),
			cfg.Elasticsearch.RequestTimeout.Value(),
		)
		if err := logsSearcher.EnsureIndex(indexContext); err != nil {
			logger.Warn("Elasticsearch logs index is not ready; startup will continue", "error", err)
		}
		cancel()
	}

	var metricsStore retrievalmetrics.Store
	var metricsSearcher *retrievalmetrics.Service
	if strings.EqualFold(cfg.Metrics.Backend, "prometheus") {
		store, err := retrievalmetrics.NewPrometheusClient(
			cfg.Metrics.BaseURL,
			cfg.Metrics.RequestTimeout.Value(),
		)
		if err != nil {
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
		metricsStore = store
		metricsSearcher, err = retrievalmetrics.NewService(metricsStore, cfg.Metrics.Queries)
		if err != nil {
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
	}

	var tracesSearcher *retrievaltraces.Service
	if strings.EqualFold(cfg.Traces.Backend, "jaeger") {
		tracesStore, err := retrievaltraces.NewJaegerClient(
			cfg.Traces.BaseURL,
			cfg.Traces.RequestTimeout.Value(),
		)
		if err != nil {
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
		tracesSearcher, err = retrievaltraces.NewService(tracesStore)
		if err != nil {
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
	}

	toolConfig := agenteino.MockToolsConfig{
		LogsBackend:           cfg.Logs.Backend,
		LogsIndex:             cfg.Logs.Index,
		LogsDefaultLimit:      cfg.Logs.DefaultLimit,
		LogsFallbackToMock:    cfg.Logs.FallbackToMock,
		LogsSearcher:          logsSearcher,
		MetricsBackend:        cfg.Metrics.Backend,
		MetricsBaseURL:        cfg.Metrics.BaseURL,
		MetricsFallbackToMock: cfg.Metrics.FallbackToMock,
		MetricsTimeout:        cfg.Metrics.RequestTimeout.Value(),
		MetricsSearcher:       metricsSearcher,
		AlertsBackend:         cfg.Metrics.Backend,
		AlertsBaseURL:         cfg.Metrics.BaseURL,
		AlertsFallbackToMock:  cfg.Metrics.FallbackToMock,
		AlertsTimeout:         cfg.Metrics.RequestTimeout.Value(),
		AlertsStore:           metricsStore,
		TracesBackend:         cfg.Traces.Backend,
		TracesBaseURL:         cfg.Traces.BaseURL,
		TracesDefaultService:  cfg.Traces.DefaultService,
		TracesDefaultLimit:    cfg.Traces.DefaultLimit,
		TracesFallbackToMock:  cfg.Traces.FallbackToMock,
		TracesTimeout:         cfg.Traces.RequestTimeout.Value(),
		TracesSearcher:        tracesSearcher,
	}
	if strings.EqualFold(cfg.Logs.Backend, "elasticsearch") {
		toolConfig.LogsTimeout = cfg.Elasticsearch.RequestTimeout.Value()
	}
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
	var longTermMemoryService *longterm.Service
	var profileLoader profile.Loader
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
		mysqlLongTermStore, err := longterm.NewMySQLStore(client.DB())
		if err != nil {
			_ = client.Close()
			_ = redisClient.Close()
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
		longTermMemoryService, err = longterm.NewService(
			mysqlLongTermStore,
			cfg.LongTermMemory.TopK,
		)
		if err != nil {
			_ = client.Close()
			_ = redisClient.Close()
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
		mysqlProfileStore, err := profile.NewMySQLStore(client.DB())
		if err != nil {
			_ = client.Close()
			_ = redisClient.Close()
			if elasticsearchClient != nil {
				_ = elasticsearchClient.Close(context.Background())
			}
			return nil, err
		}
		profileLoader, err = profile.NewManager(mysqlProfileStore)
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
			logger.Warn("MySQL application schema is not ready; startup will continue", "error", err)
		}
		cancel()
	}
	feedbackService, err := feedback.NewServiceWithLongTermMemory(
		feedbackStore,
		longTermMemoryService,
	)
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
	summarizer := buildSessionSummarizer(
		context.Background(),
		cfg,
		logger,
		newOpenAICompatibleModel,
	)
	chatService := applicationchat.NewService(
		agentRunner,
		sessionStore,
		summarizer,
		applicationchat.ServiceConfig{
			RecentWindowSize:   cfg.Session.RecentWindowSize,
			SummaryThreshold:   cfg.Session.SummaryThreshold,
			LongTermMemory:     longTermMemoryService,
			LongTermMemoryTopK: cfg.LongTermMemory.TopK,
			ProfileLoader:      profileLoader,
		},
	)
	evidenceAgent, err := multiagent.NewEvidenceAgent(
		context.Background(),
		tools,
	)
	if err != nil {
		return nil, err
	}
	knowledgeAgent, err := multiagent.NewKnowledgeAgent(
		context.Background(),
		tools,
		longTermMemoryService,
		cfg.LongTermMemory.TopK,
	)
	if err != nil {
		return nil, err
	}
	multiAgentService := multiagent.NewService(multiagent.NewOrchestrator(
		context.Background(),
		multiagent.NewDeterministicTriageAgent("checkout"),
		evidenceAgent,
		knowledgeAgent,
		multiagent.NewSynthesisAgent(nil),
	))
	evalService, err := eval.NewServiceWithRunner(
		evalStore,
		feedbackService,
		newEvalCaseExecutor(chatService),
	)
	if err != nil {
		return nil, err
	}
	router := httptransport.NewRouter(
		logger,
		cfg.Telemetry.ServiceName,
		httptransport.RouterDependencies{
			Chat:       chatService,
			MultiAgent: multiAgentService,
			Knowledge:  knowledgeService,
			Feedback:   feedbackService,
			Eval:       evalService,
			Metrics:    metricsHandler,
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
