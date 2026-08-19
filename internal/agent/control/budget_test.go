package control

import (
	"context"
	"testing"
	"time"
)

func TestExecutionBudgetNormalCompletion(t *testing.T) {
	budget := NewExecutionBudget(DefaultConfig())
	if reason := budget.StartStep(context.Background()); reason != "" {
		t.Fatalf("StartStep() = %q", reason)
	}
	if reason := budget.BeforeToolCall(context.Background(), "query_logs", `{"query":"error"}`); reason != "" {
		t.Fatalf("BeforeToolCall() = %q", reason)
	}
	if reason := budget.Complete(); reason != StopReasonCompleted {
		t.Fatalf("Complete() = %q", reason)
	}
}

func TestExecutionBudgetStopsAtMaxSteps(t *testing.T) {
	budget := NewExecutionBudget(Config{MaxIterations: 1})
	_ = budget.StartStep(context.Background())
	if reason := budget.StartStep(context.Background()); reason != StopReasonMaxStepsReached {
		t.Fatalf("reason = %q", reason)
	}
}

func TestExecutionBudgetStopsAtMaxToolCalls(t *testing.T) {
	budget := NewExecutionBudget(Config{MaxToolCalls: 1})
	_ = budget.BeforeToolCall(context.Background(), "query_logs", `{"query":"error"}`)
	if reason := budget.BeforeToolCall(context.Background(), "query_logs", `{"query":"timeout"}`); reason != StopReasonToolBudgetExceeded {
		t.Fatalf("reason = %q", reason)
	}
}

func TestExecutionBudgetDetectsOnlyEquivalentRepeatedCalls(t *testing.T) {
	budget := NewExecutionBudget(Config{
		MaxToolCalls: 10, MaxRepeatedToolCalls: 2,
		EnableRepeatedToolDetection: true,
	})
	for _, args := range []string{
		`{"service":"payment","query":"error"}`,
		`{"query":"ERROR","service":"payment"}`,
	} {
		if reason := budget.BeforeToolCall(context.Background(), "query_logs", args); reason != "" {
			t.Fatalf("equivalent call stopped too early: %q", reason)
		}
	}
	if reason := budget.BeforeToolCall(context.Background(), "query_logs", `{"query":"timeout","service":"payment"}`); reason != "" {
		t.Fatalf("different args stopped: %q", reason)
	}
	if reason := budget.BeforeToolCall(context.Background(), "query_logs", `{"service":"payment","query":"error"}`); reason != StopReasonRepeatedToolCall {
		t.Fatalf("reason = %q", reason)
	}
}

func TestExecutionBudgetTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget := NewExecutionBudget(Config{TotalExecutionTimeout: time.Hour})
	if reason := budget.StartStep(ctx); reason != StopReasonTimeout {
		t.Fatalf("reason = %q", reason)
	}
}

func TestExecutionBudgetRetryExhaustion(t *testing.T) {
	budget := NewExecutionBudget(Config{MaxRetries: 1})
	if reason := budget.RecordRetry(context.Background()); reason != "" {
		t.Fatalf("first retry = %q", reason)
	}
	if reason := budget.RecordRetry(context.Background()); reason != StopReasonRetryExhausted {
		t.Fatalf("second retry = %q", reason)
	}
}
