package bootstrap

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	sessionSummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
)

func buildSessionSummarizer(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	factory chatModelFactory,
) session.Summarizer {
	deterministic := sessionSummary.NewDeterministic()
	if !strings.EqualFold(strings.TrimSpace(cfg.Summary.Mode), "llm") {
		logger.Info("deterministic session summarizer selected")
		return deterministic
	}
	if !cfg.LLM.Enabled {
		logger.Warn("LLM is disabled; deterministic session summary fallback selected")
		return deterministic
	}

	apiKey := strings.TrimSpace(os.Getenv(cfg.LLM.APIKeyEnv))
	if strings.TrimSpace(cfg.LLM.Model) == "" || apiKey == "" {
		logger.Warn(
			"LLM configuration is incomplete; deterministic session summary fallback selected",
			"model_configured", strings.TrimSpace(cfg.LLM.Model) != "",
			"api_key_configured", apiKey != "",
		)
		return deterministic
	}
	chatModel, err := factory(ctx, cfg.LLM, apiKey)
	if err != nil {
		logger.Warn(
			"LLM summary model initialization failed; deterministic fallback selected",
			"error",
			err,
		)
		return deterministic
	}
	summarizer, err := sessionSummary.NewLLM(
		chatModel,
		deterministic,
		sessionSummary.LLMConfig{
			PromptVersion: cfg.Summary.PromptVersion,
			Timeout:       cfg.Summary.Timeout.Value(),
		},
	)
	if err != nil {
		logger.Warn(
			"LLM session summarizer initialization failed; deterministic fallback selected",
			"error",
			err,
		)
		return deterministic
	}
	logger.Info(
		"LLM session summarizer selected",
		"model",
		cfg.LLM.Model,
		"prompt_version",
		cfg.Summary.PromptVersion,
	)
	return summarizer
}
