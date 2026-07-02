package eino

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	retrievallogs "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/logs"
	retrievalmetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/metrics"
	retrievaltraces "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/traces"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type logsSearcherStub struct{}

func (logsSearcherStub) Search(
	context.Context,
	retrievallogs.SearchQuery,
) ([]retrievallogs.Event, error) {
	return []retrievallogs.Event{{
		ID:        "log_checkout_001",
		Timestamp: time.Date(2026, 6, 30, 0, 10, 0, 0, time.UTC),
		Service:   "checkout",
		Level:     "error",
		Message:   "upstream timeout calling payment",
	}}, nil
}

type metricsSearcherStub struct{}

func (metricsSearcherStub) Query(
	_ context.Context,
	request retrievalmetrics.QueryRequest,
) ([]retrievalmetrics.Sample, error) {
	return []retrievalmetrics.Sample{{
		Name:      "watchops_checkout_error_rate",
		Value:     0.062,
		Timestamp: request.At,
		Service:   request.Service,
		Labels:    map[string]string{"environment": "demo"},
		Query:     "watchops_checkout_error_rate",
	}}, nil
}

type tracesSearcherStub struct{}

func (tracesSearcherStub) Search(
	_ context.Context,
	query retrievaltraces.Query,
) ([]retrievaltraces.Span, error) {
	return []retrievaltraces.Span{{
		TraceID:    "trace-001",
		SpanID:     "span-001",
		Service:    query.Service,
		Operation:  "agent.run",
		StartTime:  query.To,
		DurationMS: 125,
	}}, nil
}

func TestBuildMockTools(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	expected := map[string]bool{
		"query_logs":           false,
		"query_metrics":        false,
		"query_alerts":         false,
		"query_traces":         false,
		"search_knowledge":     false,
		"get_service_topology": false,
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

func TestEinoAuxiliaryToolInvocationReturnsNormalizedResult(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}

	tests := []struct {
		name       string
		arguments  string
		sourceType string
	}{
		{
			name:       "query_alerts",
			sourceType: "alerts",
			arguments:  `{"service":"checkout","severity":"warning","window":"30m"}`,
		},
		{
			name:       "get_service_topology",
			sourceType: "topology",
			arguments:  `{"service":"checkout","depth":1}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assembledTool, ok := findTestTool(t, tools, test.name)
			if !ok {
				t.Fatalf("%s tool not found", test.name)
			}
			output, err := assembledTool.InvokableRun(context.Background(), test.arguments)
			if err != nil {
				t.Fatalf("InvokableRun() error = %v", err)
			}
			var result common.ToolResult
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("decode tool output: %v", err)
			}
			if !result.Success ||
				result.Tool != test.name ||
				len(result.Evidence) != 1 ||
				result.Evidence[0].SourceType != test.sourceType {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func findTestTool(
	t *testing.T,
	tools []einotool.InvokableTool,
	name string,
) (einotool.InvokableTool, bool) {
	t.Helper()
	for _, assembledTool := range tools {
		info, infoErr := assembledTool.Info(context.Background())
		if infoErr != nil {
			t.Fatalf("tool Info() error = %v", infoErr)
		}
		if info.Name == name {
			return assembledTool, true
		}
	}
	return nil, false
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

func TestEinoToolInvocationUsesConfiguredElasticsearchLogs(t *testing.T) {
	tools, err := BuildMockToolsWithConfig(MockToolsConfig{
		LogsBackend:        "elasticsearch",
		LogsIndex:          "watchops_logs",
		LogsDefaultLimit:   20,
		LogsFallbackToMock: true,
		LogsSearcher:       logsSearcherStub{},
	})
	if err != nil {
		t.Fatalf("BuildMockToolsWithConfig() error = %v", err)
	}

	for _, assembledTool := range tools {
		info, infoErr := assembledTool.Info(context.Background())
		if infoErr != nil {
			t.Fatalf("tool Info() error = %v", infoErr)
		}
		if info.Name != "query_logs" {
			continue
		}
		output, invokeErr := assembledTool.InvokableRun(context.Background(), `{
			"service":"checkout",
			"time_range":{
				"from":"2026-06-30T00:00:00Z",
				"to":"2026-06-30T00:20:00Z"
			},
			"keywords":["timeout"],
			"level":"error"
		}`)
		if invokeErr != nil {
			t.Fatalf("InvokableRun() error = %v", invokeErr)
		}
		var result common.ToolResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("decode tool output: %v", err)
		}
		if len(result.Evidence) != 1 ||
			result.Evidence[0].SourceName != "elasticsearch-logs" {
			t.Fatalf("result = %#v", result)
		}
		return
	}
	t.Fatal("query_logs tool not found")
}

func TestEinoToolInvocationUsesConfiguredPrometheusMetrics(t *testing.T) {
	tools, err := BuildMockToolsWithConfig(MockToolsConfig{
		MetricsBackend:        "prometheus",
		MetricsBaseURL:        "http://localhost:9090",
		MetricsFallbackToMock: true,
		MetricsSearcher:       metricsSearcherStub{},
	})
	if err != nil {
		t.Fatalf("BuildMockToolsWithConfig() error = %v", err)
	}

	for _, assembledTool := range tools {
		info, infoErr := assembledTool.Info(context.Background())
		if infoErr != nil {
			t.Fatalf("tool Info() error = %v", infoErr)
		}
		if info.Name != "query_metrics" {
			continue
		}
		output, invokeErr := assembledTool.InvokableRun(context.Background(), `{
			"service":"checkout",
			"metric_name":"http_server_error_rate",
			"time_range":{
				"from":"2026-06-30T00:00:00Z",
				"to":"2026-06-30T00:20:00Z"
			}
		}`)
		if invokeErr != nil {
			t.Fatalf("InvokableRun() error = %v", invokeErr)
		}
		var result common.ToolResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("decode tool output: %v", err)
		}
		if len(result.Evidence) != 1 ||
			result.Evidence[0].SourceName != "prometheus" {
			t.Fatalf("result = %#v", result)
		}
		return
	}
	t.Fatal("query_metrics tool not found")
}

func TestEinoToolInvocationUsesConfiguredJaegerTraces(t *testing.T) {
	tools, err := BuildMockToolsWithConfig(MockToolsConfig{
		TracesBackend:        "jaeger",
		TracesBaseURL:        "http://localhost:16686",
		TracesDefaultService: "watchops-lite",
		TracesDefaultLimit:   10,
		TracesFallbackToMock: true,
		TracesSearcher:       tracesSearcherStub{},
	})
	if err != nil {
		t.Fatalf("BuildMockToolsWithConfig() error = %v", err)
	}

	for _, assembledTool := range tools {
		info, infoErr := assembledTool.Info(context.Background())
		if infoErr != nil {
			t.Fatalf("tool Info() error = %v", infoErr)
		}
		if info.Name != "query_traces" {
			continue
		}
		output, invokeErr := assembledTool.InvokableRun(context.Background(), `{
			"service":"watchops-lite",
			"trace_id":"trace-001",
			"time_range":{
				"from":"2026-07-01T05:00:00Z",
				"to":"2026-07-01T05:20:00Z"
			}
		}`)
		if invokeErr != nil {
			t.Fatalf("InvokableRun() error = %v", invokeErr)
		}
		var result common.ToolResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("decode tool output: %v", err)
		}
		if len(result.Evidence) != 1 || result.Evidence[0].SourceName != "jaeger" {
			t.Fatalf("result = %#v", result)
		}
		return
	}
	t.Fatal("query_traces tool not found")
}
