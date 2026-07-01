package bootstrap

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	sessionSummary "github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session/summary"
)

func TestBuildSessionSummarizerUsesDeterministicMode(t *testing.T) {
	cfg := config.Default()

	summarizer := buildSessionSummarizer(
		context.Background(),
		cfg,
		testAgentLogger(),
		func(context.Context, config.LLMConfig, string) (model.ToolCallingChatModel, error) {
			t.Fatal("model factory must not run in deterministic summary mode")
			return nil, nil
		},
	)
	if _, ok := summarizer.(*sessionSummary.Deterministic); !ok {
		t.Fatalf("summarizer type = %T, want deterministic", summarizer)
	}
}

func TestBuildSessionSummarizerSelectsLLMWhenConfigured(t *testing.T) {
	cfg := config.Default()
	cfg.Summary.Mode = "llm"
	cfg.LLM.Enabled = true
	cfg.LLM.Model = "summary-model"
	t.Setenv(cfg.LLM.APIKeyEnv, "test-key")

	summarizer := buildSessionSummarizer(
		context.Background(),
		cfg,
		testAgentLogger(),
		func(context.Context, config.LLMConfig, string) (model.ToolCallingChatModel, error) {
			return bootstrapModelStub{}, nil
		},
	)
	if _, ok := summarizer.(*sessionSummary.LLM); !ok {
		t.Fatalf("summarizer type = %T, want LLM", summarizer)
	}
}
