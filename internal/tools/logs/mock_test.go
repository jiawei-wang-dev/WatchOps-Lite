package logs

import (
	"context"
	"errors"
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
		Keywords: []string{"timeout"},
		Level:    "error",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success || result.Tool != Name || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v, want one successful log evidence item", result)
	}
}

func TestMockToolInvalidInput(t *testing.T) {
	result, err := NewMockTool(0).Execute(context.Background(), Input{})

	var toolErr *common.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("error = %v, want *common.ToolError", err)
	}
	if toolErr.Code != common.ErrorCodeInvalidArgument {
		t.Fatalf("error code = %q, want %q", toolErr.Code, common.ErrorCodeInvalidArgument)
	}
	if toolErr.Tool == "" || toolErr.Message == "" || toolErr.Fallback == "" {
		t.Fatalf("ToolError fields not populated: %#v", toolErr)
	}
	if result.Success || result.Error == nil {
		t.Fatalf("result = %#v, want structured failure", result)
	}
}
