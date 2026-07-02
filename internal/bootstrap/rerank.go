package bootstrap

import (
	"log/slog"
	"os"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/rerank"
)

func buildReranker(
	cfg config.Config,
	logger *slog.Logger,
) rerank.Reranker {
	if !cfg.Rerank.Enabled {
		return nil
	}
	ruleBased := rerank.NewRuleBased()
	if strings.EqualFold(cfg.Rerank.Provider, "rule") {
		return ruleBased
	}

	apiKey := strings.TrimSpace(os.Getenv(cfg.Rerank.APIKeyEnv))
	if apiKey == "" {
		logger.Warn(
			"external rerank API key is unavailable; rule-based rerank fallback selected",
			"api_key_env",
			cfg.Rerank.APIKeyEnv,
		)
		composite, _ := rerank.NewComposite(nil, ruleBased)
		return composite
	}
	external, err := rerank.NewExternal(rerank.ExternalConfig{
		BaseURL: cfg.Rerank.BaseURL,
		APIKey:  apiKey,
		Model:   cfg.Rerank.Model,
		Timeout: cfg.Rerank.Timeout.Value(),
	})
	if err != nil {
		logger.Warn(
			"external rerank provider initialization failed; rule-based fallback selected",
			"error",
			err,
		)
		composite, _ := rerank.NewComposite(nil, ruleBased)
		return composite
	}
	composite, _ := rerank.NewComposite(external, ruleBased)
	return composite
}
