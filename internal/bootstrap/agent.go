package bootstrap

import (
	"context"
	"log/slog"
	"os"
	"strings"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/control"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/multiagent"
)

type chatModelFactory func(
	context.Context,
	config.LLMConfig,
	string,
) (model.ToolCallingChatModel, error)

func buildAgentRunner(
	ctx context.Context,
	cfg config.Config,
	tools []einotool.InvokableTool,
	logger *slog.Logger,
	factory chatModelFactory,
) agenteino.AgentRunner {
	deterministic := agenteino.NewDeterministicRunner(tools)
	if strings.ToLower(strings.TrimSpace(cfg.Agent.Mode)) != "eino_react" {
		logger.Info("deterministic Agent runner selected")
		return deterministic
	}
	// LLM availability is checked before constructing Eino so local demos,
	// tests, and degraded environments keep the same deterministic behavior.
	if !cfg.LLM.Enabled {
		logger.Warn("LLM is disabled; deterministic Agent fallback selected")
		return deterministic
	}

	apiKey := strings.TrimSpace(os.Getenv(cfg.LLM.APIKeyEnv))
	if strings.TrimSpace(cfg.LLM.Model) == "" || apiKey == "" {
		logger.Warn(
			"LLM configuration is incomplete; deterministic Agent fallback selected",
			"model_configured", strings.TrimSpace(cfg.LLM.Model) != "",
			"api_key_configured", apiKey != "",
		)
		return deterministic
	}
	chatModel, err := factory(ctx, cfg.LLM, apiKey)
	if err != nil {
		logger.Warn("LLM model initialization failed; deterministic Agent fallback selected", "error", err)
		return deterministic
	}
	einoRunner, err := agenteino.NewReActRunner(ctx, chatModel, tools, agenteino.ReActRunnerConfig{
		MaxIterations: cfg.Agent.MaxIterations,
		Timeout:       cfg.Agent.Timeout.Value(),
		PromptVersion: cfg.Agent.PromptVersion,
		ModelName:     cfg.LLM.Model,
		Control:       agentControlConfig(cfg.Agent),
	})
	if err != nil {
		logger.Warn("Eino ReAct Agent initialization failed; deterministic fallback selected", "error", err)
		return deterministic
	}
	logger.Info(
		"Eino ReAct Agent selected",
		"model", cfg.LLM.Model,
		"prompt_version", cfg.Agent.PromptVersion,
		"max_iterations", cfg.Agent.MaxIterations,
	)
	// The deterministic runner remains in the hot path as a safety net for
	// model errors, invalid final JSON, and failure-controller boundaries.
	return agenteino.NewFallbackRunnerWithControl(
		einoRunner,
		deterministic,
		agentControlConfig(cfg.Agent),
	)
}

func buildMultiAgentRoleLLM(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	factory chatModelFactory,
) *multiagent.RoleLLM {
	// Multi-Agent roles share the same ChatModel adapter, but each role still
	// validates its own JSON boundary so a weak role output never poisons the
	// rest of the graph.
	if !cfg.LLM.Enabled {
		logger.Info("Multi-Agent role LLM analysis disabled")
		return nil
	}
	apiKey := strings.TrimSpace(os.Getenv(cfg.LLM.APIKeyEnv))
	if strings.TrimSpace(cfg.LLM.Model) == "" || apiKey == "" {
		logger.Warn(
			"Multi-Agent role LLM configuration is incomplete; deterministic role analysis selected",
			"model_configured", strings.TrimSpace(cfg.LLM.Model) != "",
			"api_key_configured", apiKey != "",
		)
		return nil
	}
	chatModel, err := factory(ctx, cfg.LLM, apiKey)
	if err != nil {
		logger.Warn(
			"Multi-Agent role LLM initialization failed; deterministic role analysis selected",
			"error", err,
		)
		return nil
	}
	analyzer, err := multiagent.NewRoleLLM(
		chatModel,
		cfg.LLM.Model,
		cfg.LLM.RequestTimeout.Value(),
	)
	if err != nil {
		logger.Warn(
			"Multi-Agent role LLM configuration failed; deterministic role analysis selected",
			"error", err,
		)
		return nil
	}
	logger.Info("Multi-Agent role LLM analysis enabled", "model", cfg.LLM.Model)
	return analyzer
}

func agentControlConfig(cfg config.AgentConfig) control.Config {
	return control.Config{
		MaxIterations:               cfg.MaxIterations,
		MaxToolCalls:                cfg.MaxToolCalls,
		MaxConsecutiveToolFailures:  cfg.MaxConsecutiveToolFailures,
		TotalExecutionTimeout:       cfg.TotalExecutionTimeout.Value(),
		EnableJSONRepairOnce:        cfg.EnableJSONRepairOnce,
		EnableRepeatedToolDetection: cfg.EnableRepeatedToolDetection,
	}
}

func newOpenAICompatibleModel(
	ctx context.Context,
	cfg config.LLMConfig,
	apiKey string,
) (model.ToolCallingChatModel, error) {
	temperature := float32(cfg.Temperature)
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:      apiKey,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		Temperature: &temperature,
		Timeout:     cfg.RequestTimeout.Value(),
	})
}
