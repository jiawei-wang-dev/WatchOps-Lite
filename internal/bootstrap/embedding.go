package bootstrap

import (
	"log/slog"
	"os"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/embedding"
)

func buildEmbeddingProvider(
	cfg config.Config,
	logger *slog.Logger,
) embedding.Provider {
	if !cfg.Embedding.Enabled {
		return nil
	}
	apiKey := strings.TrimSpace(os.Getenv(cfg.Embedding.APIKeyEnv))
	if apiKey == "" {
		logger.Warn(
			"embedding API key is unavailable; knowledge retrieval will use configured fallback",
			"api_key_env",
			cfg.Embedding.APIKeyEnv,
		)
		return nil
	}
	provider, err := embedding.NewOpenAICompatible(embedding.OpenAICompatibleConfig{
		BaseURL:   cfg.Embedding.BaseURL,
		APIKey:    apiKey,
		Model:     cfg.Embedding.Model,
		Dimension: cfg.Embedding.Dimension,
		Timeout:   cfg.Embedding.RequestTimeout.Value(),
	})
	if err != nil {
		logger.Warn(
			"embedding provider initialization failed; knowledge retrieval will use configured fallback",
			"error",
			err,
		)
		return nil
	}
	return provider
}
