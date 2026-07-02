package guard_test

import (
	"context"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/guard"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/alerts"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/topology"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/traces"
)

func TestGuardRejectsInvalidInputs(t *testing.T) {
	toolGuard := guard.Default()
	validRange := common.TimeRange{
		From: "2026-06-30T00:00:00Z",
		To:   "2026-06-30T00:20:00Z",
	}
	tests := []struct {
		name  string
		tool  string
		input any
	}{
		{"unknown tool", "delete_service", metrics.Input{Service: "checkout", MetricName: "x", TimeRange: validRange}},
		{"invalid service", metrics.Name, metrics.Input{Service: "checkout prod", MetricName: "x", TimeRange: validRange}},
		{"excessive time window", logs.Name, logs.Input{Service: "checkout", TimeRange: common.TimeRange{From: "2026-06-30T00:00:00Z", To: "2026-07-02T00:00:01Z"}}},
		{"invalid trace id", traces.Name, traces.Input{Service: "checkout", TraceID: "trace-123", TimeRange: validRange}},
		{"invalid severity", alerts.Name, alerts.Input{Service: "checkout", Severity: "urgent"}},
		{"excessive topology depth", topology.Name, topology.Input{Service: "checkout", Depth: 4}},
		{"excessive top k", knowledge.Name, knowledge.Input{Query: "checkout", TopK: 11}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := toolGuard.Validate(context.Background(), test.tool, test.input)
			if result.Allowed || result.Error == nil {
				t.Fatalf("Validate() = %#v, want rejection", result)
			}
			if result.Error.Code != toolruntime.ErrorCodeInvalidArgument ||
				result.Error.Details["error_type"] != "validation_error" {
				t.Fatalf("error = %#v", result.Error)
			}
		})
	}
}

func TestGuardAllowsCurrentReadOnlyTools(t *testing.T) {
	toolGuard := guard.Default()
	validRange := common.TimeRange{
		From: "2026-06-30T00:00:00Z",
		To:   "2026-06-30T00:20:00Z",
	}
	tests := []struct {
		tool  string
		input any
	}{
		{metrics.Name, metrics.Input{Service: "checkout", MetricName: "http_server_error_rate", TimeRange: validRange}},
		{logs.Name, logs.Input{Service: "checkout", TimeRange: validRange, Level: "error"}},
		{traces.Name, traces.Input{Service: "checkout", TraceID: "9df0c1f254cffbe547fc944e821871d0", TimeRange: validRange}},
		{knowledge.Name, knowledge.Input{Query: "checkout timeout runbook", TopK: 3}},
		{alerts.Name, alerts.Input{Service: "checkout", Severity: "warning", Window: "30m"}},
		{topology.Name, topology.Input{Service: "checkout", Depth: 1}},
	}
	for _, test := range tests {
		t.Run(test.tool, func(t *testing.T) {
			result := toolGuard.Validate(context.Background(), test.tool, test.input)
			if !result.Allowed || result.Error != nil {
				t.Fatalf("Validate() = %#v, want allowed", result)
			}
		})
	}
}

func TestGuardRedactsSensitiveKeys(t *testing.T) {
	toolGuard := guard.Default()
	result := toolGuard.SanitizeResult(toolruntime.Result{
		Payload: map[string]any{
			"api_key": "should-not-leak",
			"nested":  map[string]any{"password": "secret"},
		},
		Metadata: map[string]any{"authorization": "Bearer token"},
		Evidence: []evidence.Item{{
			ID:      "evidence-1",
			Source:  evidence.SourceLogs,
			Content: "content",
			Metadata: map[string]any{
				"cookie":  "session=value",
				"service": "checkout",
			},
		}},
		Error: toolruntime.NewToolError(
			toolruntime.ErrorCodeInvalidArgument,
			"query_logs",
			"safe error",
			false,
			map[string]any{"credential": "raw"},
		),
	})

	if result.Payload["api_key"] != guard.RedactedValue ||
		result.Payload["nested"].(map[string]any)["password"] != guard.RedactedValue ||
		result.Metadata["authorization"] != guard.RedactedValue ||
		result.Evidence[0].Metadata["cookie"] != guard.RedactedValue ||
		result.Error.Details["credential"] != guard.RedactedValue {
		t.Fatalf("sanitized result = %#v", result)
	}
}

func TestWindowAllowsTwentyFourHours(t *testing.T) {
	toolGuard := guard.Default()
	result := toolGuard.Validate(context.Background(), alerts.Name, alerts.Input{
		Service: "checkout",
		Window:  (24 * time.Hour).String(),
	})
	if !result.Allowed {
		t.Fatalf("Validate() = %#v, want 24h allowed", result)
	}
}
