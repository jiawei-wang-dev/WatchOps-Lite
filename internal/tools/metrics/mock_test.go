package metrics

import (
	"context"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestMockToolSuccess(t *testing.T) {
	result, err := NewMockTool(0).Execute(context.Background(), Input{
		Service:    "checkout",
		MetricName: "http_server_request_duration",
		TimeRange: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success || result.Tool != Name || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v, want one successful metric evidence item", result)
	}
}
