package eino

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestBuildMockTools(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	expected := map[string]bool{
		"query_logs":       false,
		"query_metrics":    false,
		"query_traces":     false,
		"search_knowledge": false,
	}
	if len(tools) != len(expected) {
		t.Fatalf("tool count = %d, want %d", len(tools), len(expected))
	}

	for _, assembledTool := range tools {
		info, err := assembledTool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool Info() error = %v", err)
		}
		if _, ok := expected[info.Name]; !ok {
			t.Fatalf("unexpected tool name %q", info.Name)
		}
		if expected[info.Name] {
			t.Fatalf("duplicate tool name %q", info.Name)
		}
		if info.ParamsOneOf == nil {
			t.Fatalf("tool %q has no inferred parameter schema", info.Name)
		}
		expected[info.Name] = true
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("tool %q was not assembled", name)
		}
	}
}

func TestEinoToolInvocationReturnsNormalizedResult(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	var logsToolIndex = -1
	for index, assembledTool := range tools {
		info, infoErr := assembledTool.Info(context.Background())
		if infoErr != nil {
			t.Fatalf("tool Info() error = %v", infoErr)
		}
		if info.Name == "query_logs" {
			logsToolIndex = index
			break
		}
	}
	if logsToolIndex == -1 {
		t.Fatal("query_logs tool not found")
	}

	output, err := tools[logsToolIndex].InvokableRun(
		context.Background(),
		`{
			"service": "checkout",
			"time_range": {
				"from": "2026-06-30T00:00:00Z",
				"to": "2026-06-30T00:20:00Z"
			},
			"level": "error"
		}`,
	)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}

	var result common.ToolResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode tool output: %v", err)
	}
	if !result.Success || result.Tool != "query_logs" || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v, want normalized query_logs result", result)
	}
}

func TestEinoToolInvocationPreservesStructuredError(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	for _, assembledTool := range tools {
		info, infoErr := assembledTool.Info(context.Background())
		if infoErr != nil {
			t.Fatalf("tool Info() error = %v", infoErr)
		}
		if info.Name != "query_logs" {
			continue
		}

		_, invokeErr := assembledTool.InvokableRun(context.Background(), `{}`)
		var toolErr *common.ToolError
		if !errors.As(invokeErr, &toolErr) {
			t.Fatalf("error = %v, want wrapped *common.ToolError", invokeErr)
		}
		if toolErr.Code != common.ErrorCodeInvalidArgument || toolErr.Tool != "query_logs" {
			t.Fatalf("ToolError = %#v, want query_logs invalid argument", toolErr)
		}
		return
	}

	t.Fatal("query_logs tool not found")
}
