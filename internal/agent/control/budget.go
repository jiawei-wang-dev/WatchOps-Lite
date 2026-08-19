package control

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// StopReason is the stable, observable terminal state of one Agent turn.
type StopReason string

const (
	StopReasonCompleted             StopReason = "completed"
	StopReasonClarificationRequired StopReason = "clarification_required"
	StopReasonMaxStepsReached       StopReason = "max_steps_reached"
	StopReasonToolBudgetExceeded    StopReason = "tool_budget_exceeded"
	StopReasonRepeatedToolCall      StopReason = "repeated_tool_call"
	StopReasonTimeout               StopReason = "timeout"
	StopReasonToolFailure           StopReason = "tool_failure"
	StopReasonModelFailure          StopReason = "model_failure"
	StopReasonRetryExhausted        StopReason = "retry_exhausted"
)

type BudgetSnapshot struct {
	AgentSteps    int           `json:"agent_steps"`
	ToolCalls     int           `json:"tool_calls"`
	Retries       int           `json:"retries"`
	RepeatedCalls int           `json:"repeated_calls"`
	StopReason    StopReason    `json:"stop_reason,omitempty"`
	Stopped       bool          `json:"stopped"`
	Elapsed       time.Duration `json:"elapsed"`
}

// ExecutionBudget is a concurrency-safe turn budget. It performs checks before
// work starts so a limit is not merely reported after an expensive call.
type ExecutionBudget struct {
	mu           sync.Mutex
	config       Config
	started      time.Time
	steps        int
	toolCalls    int
	retries      int
	repeated     int
	fingerprints map[string]int
	stopReason   StopReason
}

func NewExecutionBudget(config Config) *ExecutionBudget {
	return &ExecutionBudget{
		config:       Normalize(config),
		started:      time.Now(),
		fingerprints: map[string]int{},
	}
}

func (b *ExecutionBudget) StartStep(ctx context.Context) StopReason {
	b.mu.Lock()
	defer b.mu.Unlock()
	if reason := b.checkStopped(ctx); reason != "" {
		return reason
	}
	if b.steps >= b.config.MaxIterations {
		return b.stop(StopReasonMaxStepsReached)
	}
	b.steps++
	return ""
}

func (b *ExecutionBudget) BeforeToolCall(
	ctx context.Context,
	toolName string,
	arguments string,
) StopReason {
	b.mu.Lock()
	defer b.mu.Unlock()
	if reason := b.checkStopped(ctx); reason != "" {
		return reason
	}
	if b.toolCalls >= b.config.MaxToolCalls {
		return b.stop(StopReasonToolBudgetExceeded)
	}
	fingerprint := ToolCallFingerprint(toolName, arguments)
	if fingerprint != "" {
		b.fingerprints[fingerprint]++
		if b.fingerprints[fingerprint] > 1 {
			b.repeated++
		}
		if b.config.EnableRepeatedToolDetection &&
			b.fingerprints[fingerprint] > b.config.MaxRepeatedToolCalls {
			return b.stop(StopReasonRepeatedToolCall)
		}
	}
	b.toolCalls++
	return ""
}

func (b *ExecutionBudget) RecordRetry(ctx context.Context) StopReason {
	b.mu.Lock()
	defer b.mu.Unlock()
	if reason := b.checkStopped(ctx); reason != "" {
		return reason
	}
	if b.retries >= b.config.MaxRetries {
		return b.stop(StopReasonRetryExhausted)
	}
	b.retries++
	return ""
}

func (b *ExecutionBudget) Complete() StopReason {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopReason == "" {
		b.stopReason = StopReasonCompleted
	}
	return b.stopReason
}

func (b *ExecutionBudget) Stop(reason StopReason) StopReason {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stop(reason)
}

func (b *ExecutionBudget) Snapshot() BudgetSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BudgetSnapshot{
		AgentSteps: b.steps, ToolCalls: b.toolCalls, Retries: b.retries,
		RepeatedCalls: b.repeated, StopReason: b.stopReason,
		Stopped: b.stopReason != "" && b.stopReason != StopReasonCompleted,
		Elapsed: time.Since(b.started),
	}
}

func (b *ExecutionBudget) checkStopped(ctx context.Context) StopReason {
	if b.stopReason != "" {
		return b.stopReason
	}
	if ctx != nil && ctx.Err() != nil {
		return b.stop(StopReasonTimeout)
	}
	if time.Since(b.started) >= b.config.TotalExecutionTimeout {
		return b.stop(StopReasonTimeout)
	}
	return ""
}

func (b *ExecutionBudget) stop(reason StopReason) StopReason {
	if b.stopReason == "" {
		b.stopReason = reason
	}
	return b.stopReason
}

// ToolCallFingerprint is stable across JSON key order, insignificant spacing,
// and argument-object ordering, while preserving semantically different args.
func ToolCallFingerprint(toolName, arguments string) string {
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	if toolName == "" {
		return ""
	}
	var value any
	if json.Unmarshal([]byte(arguments), &value) != nil {
		return toolName + "|" + strings.Join(strings.Fields(strings.ToLower(arguments)), " ")
	}
	return toolName + "|" + canonicalJSON(value)
}

func canonicalJSON(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+":"+canonicalJSON(typed[key]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, canonicalJSON(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case string:
		encoded, _ := json.Marshal(strings.Join(strings.Fields(strings.ToLower(typed)), " "))
		return string(encoded)
	default:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	}
}
