package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
)

type bootstrapModelStub struct{}

func (bootstrapModelStub) Generate(
	context.Context,
	[]*schema.Message,
	...model.Option,
) (*schema.Message, error) {
	return nil, errors.New("not called during runner selection")
}

func (bootstrapModelStub) Stream(
	context.Context,
	[]*schema.Message,
	...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not called during runner selection")
}

func (m bootstrapModelStub) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func TestBuildAgentRunnerUsesDeterministicWhenLLMDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Mode = "eino_react"
	cfg.LLM.Enabled = false
	tools, _ := agenteino.BuildMockTools()

	runner := buildAgentRunner(
		context.Background(),
		cfg,
		tools,
		testAgentLogger(),
		func(context.Context, config.LLMConfig, string) (model.ToolCallingChatModel, error) {
			t.Fatal("model factory must not run when LLM is disabled")
			return nil, nil
		},
	)
	if _, ok := runner.(*agenteino.DeterministicRunner); !ok {
		t.Fatalf("runner type = %T, want deterministic", runner)
	}
}

func TestBuildAgentRunnerSelectsEinoWhenConfigIsValid(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Mode = "eino_react"
	cfg.LLM.Enabled = true
	cfg.LLM.Model = "test-model"
	t.Setenv(cfg.LLM.APIKeyEnv, "test-key")
	tools, _ := agenteino.BuildMockTools()
	factoryCalled := false

	runner := buildAgentRunner(
		context.Background(),
		cfg,
		tools,
		testAgentLogger(),
		func(context.Context, config.LLMConfig, string) (model.ToolCallingChatModel, error) {
			factoryCalled = true
			return bootstrapModelStub{}, nil
		},
	)
	if !factoryCalled {
		t.Fatal("model factory was not called")
	}
	if _, ok := runner.(*agenteino.FallbackRunner); !ok {
		t.Fatalf("runner type = %T, want Eino runner with fallback policy", runner)
	}
}

func TestBuildAgentRunnerFallsBackForMissingLLMConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Mode = "eino_react"
	cfg.LLM.Enabled = true
	cfg.LLM.Model = ""
	t.Setenv(cfg.LLM.APIKeyEnv, "")
	tools, _ := agenteino.BuildMockTools()
	factoryCalled := false

	runner := buildAgentRunner(
		context.Background(),
		cfg,
		tools,
		testAgentLogger(),
		func(context.Context, config.LLMConfig, string) (model.ToolCallingChatModel, error) {
			factoryCalled = true
			return bootstrapModelStub{}, nil
		},
	)
	if factoryCalled {
		t.Fatal("model factory ran with incomplete configuration")
	}
	if _, ok := runner.(*agenteino.DeterministicRunner); !ok {
		t.Fatalf("runner type = %T, want deterministic fallback", runner)
	}
}

func testAgentLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
