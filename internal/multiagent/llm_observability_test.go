package multiagent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClassifyLLMError(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		err    error
		want   string
	}{
		{
			name: "deadline",
			err:  context.DeadlineExceeded,
			want: "deadline_exceeded",
		},
		{
			name:   "parse",
			reason: "invalid_json",
			err:    errors.New("bad json"),
			want:   "parse_failed",
		},
		{
			name: "rate limit",
			err:  errors.New("429 too many requests"),
			want: "rate_limited",
		},
		{
			name: "auth",
			err:  errors.New("401 unauthorized"),
			want: "auth_failed",
		},
		{
			name: "provider",
			err:  errors.New("connection refused"),
			want: "provider_unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyLLMError(tt.reason, tt.err); got != tt.want {
				t.Fatalf("classifyLLMError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAggregateLLMExecutionDegraded(t *testing.T) {
	result := aggregateLLMExecution([]roleExecutionMetadata{
		{
			role: "triage",
			metadata: roleLLMMetadata(roleLLMMetadataInput{
				Role:      AgentRoleTriage,
				Model:     "deepseek-v4-flash",
				Attempted: true,
				Success:   true,
				Call: llmCallResult{
					durationMS: 120,
					timeout:    20 * time.Second,
				},
			}),
		},
		{
			role: "evidence",
			metadata: roleLLMMetadata(roleLLMMetadataInput{
				Role:      AgentRoleEvidence,
				Model:     "deepseek-v4-flash",
				Attempted: true,
				Success:   false,
				Call: llmCallResult{
					durationMS:   20000,
					timeout:      20 * time.Second,
					errorCode:    "deadline_exceeded",
					errorMessage: "context deadline exceeded",
				},
				Fallback:       true,
				FallbackReason: "evidence_llm_analysis_failed",
			}),
		},
	})
	if result["llm_execution_status"] != "degraded" {
		t.Fatalf("status = %#v", result)
	}
	if result["llm_success_count"] != 1 || result["llm_failure_count"] != 1 {
		t.Fatalf("counts = %#v", result)
	}
}

func TestAggregateLLMExecutionParseFailedKeepsProviderAvailable(t *testing.T) {
	result := aggregateLLMExecution([]roleExecutionMetadata{
		{
			role: "knowledge",
			metadata: roleLLMMetadata(roleLLMMetadataInput{
				Role:      AgentRoleKnowledge,
				Model:     "deepseek-v4-flash",
				Attempted: true,
				Success:   false,
				Call: llmCallResult{
					durationMS:   8620,
					timeout:      20 * time.Second,
					errorCode:    "parse_failed",
					errorMessage: "json cannot unmarshal string into []string",
					retryCount:   1,
				},
				Fallback: true,
			}),
		},
	})
	if result["llm_execution_status"] != "degraded" {
		t.Fatalf("status = %#v", result)
	}
	if result["llm_available"] != true {
		t.Fatalf("available = %#v", result)
	}
	if result["llm_retry_count"] != 1 ||
		result["llm_call_count"] != 2 ||
		result["llm_success_count"] != 0 ||
		result["llm_failure_count"] != 1 {
		t.Fatalf("counts = %#v", result)
	}
}

func TestAggregateLLMExecutionProviderUnavailable(t *testing.T) {
	result := aggregateLLMExecution([]roleExecutionMetadata{
		{
			role: "knowledge",
			metadata: roleLLMMetadata(roleLLMMetadataInput{
				Role:      AgentRoleKnowledge,
				Model:     "deepseek-v4-flash",
				Attempted: true,
				Success:   false,
				Call: llmCallResult{
					errorCode: "provider_unavailable",
				},
				Fallback: true,
			}),
		},
	})
	if result["llm_execution_status"] != "unavailable" {
		t.Fatalf("status = %#v", result)
	}
	if result["llm_available"] != false {
		t.Fatalf("available = %#v", result)
	}
}

func TestAggregateLLMExecutionNotConfigured(t *testing.T) {
	result := aggregateLLMExecution([]roleExecutionMetadata{
		{
			role:     "triage",
			metadata: roleLLMNotConfiguredMetadata(AgentRoleTriage, "triage_llm_not_configured"),
		},
	})
	if result["llm_execution_status"] != "not_configured" {
		t.Fatalf("status = %#v", result)
	}
	if result["llm_attempt_count"] != 0 {
		t.Fatalf("attempt count = %#v", result)
	}
}
