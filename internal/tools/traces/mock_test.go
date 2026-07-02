package traces

import (
	"context"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestMockToolSuccess(t *testing.T) {
	result, err := NewMockTool(0).Execute(context.Background(), Input{
		Service: "checkout",
		TimeRange: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
		TraceID: "9df0c1f254cffbe547fc944e821871d0",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success || result.Tool != Name || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v, want one successful trace evidence item", result)
	}
}
