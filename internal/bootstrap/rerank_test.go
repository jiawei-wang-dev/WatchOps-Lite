package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/rerank"
)

func TestBuildRerankerModes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	disabled := config.Default()
	disabled.Rerank.Enabled = false
	if result := buildReranker(disabled, logger); result != nil {
		t.Fatalf("disabled reranker = %#v, want nil", result)
	}

	ruleConfig := config.Default()
	if _, ok := buildReranker(ruleConfig, logger).(*rerank.RuleBasedReranker); !ok {
		t.Fatalf("rule provider did not build RuleBasedReranker")
	}

	externalConfig := config.Default()
	externalConfig.Rerank.Provider = "external"
	externalConfig.Rerank.BaseURL = "https://rerank.test/v1"
	externalConfig.Rerank.Model = "rerank-test"
	t.Setenv(externalConfig.Rerank.APIKeyEnv, "")
	composite := buildReranker(externalConfig, logger)
	results, err := composite.Rerank(
		context.Background(),
		"checkout runbook",
		[]rerank.Candidate{{
			ID: "runbook", Title: "Checkout runbook", Content: "Inspect latency.", Score: 1,
		}},
		1,
	)
	if err != nil {
		t.Fatalf("fallback Rerank() error = %v", err)
	}
	if len(results) != 1 ||
		results[0].Provider != "rule_based" ||
		results[0].FallbackReason != "external_not_configured" {
		t.Fatalf("results = %#v", results)
	}
}
