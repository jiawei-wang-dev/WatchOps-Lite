package eino

import (
	"context"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/alerts"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/topology"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/traces"
)

type MockToolsConfig struct {
	LogsTimeout           time.Duration
	LogsBackend           string
	LogsIndex             string
	LogsDefaultLimit      int
	LogsFallbackToMock    bool
	LogsSearcher          logs.Searcher
	MetricsBackend        string
	MetricsBaseURL        string
	MetricsFallbackToMock bool
	MetricsTimeout        time.Duration
	MetricsSearcher       metrics.Searcher
	AlertsBackend         string
	AlertsBaseURL         string
	AlertsFallbackToMock  bool
	AlertsTimeout         time.Duration
	AlertsStore           alerts.Store
	TopologyTimeout       time.Duration
	TracesBackend         string
	TracesBaseURL         string
	TracesDefaultService  string
	TracesDefaultLimit    int
	TracesFallbackToMock  bool
	TracesTimeout         time.Duration
	TracesSearcher        traces.Searcher
	KnowledgeTimeout      time.Duration
	KnowledgeSearcher     knowledge.Searcher
}

func BuildMockTools() ([]einotool.InvokableTool, error) {
	return BuildMockToolsWithConfig(MockToolsConfig{})
}

func BuildMockToolsWithConfig(config MockToolsConfig) ([]einotool.InvokableTool, error) {
	logsRuntime := logs.NewMockTool(config.LogsTimeout).Runtime()
	if strings.EqualFold(config.LogsBackend, "elasticsearch") {
		logsRuntime = logs.NewSearchTool(config.LogsSearcher, logs.SearchToolConfig{
			Backend:        config.LogsBackend,
			Index:          config.LogsIndex,
			DefaultLimit:   config.LogsDefaultLimit,
			FallbackToMock: config.LogsFallbackToMock,
			Timeout:        config.LogsTimeout,
		}).Runtime()
	}
	logsTool, err := toolutils.InferTool(
		logs.Name,
		"Query service logs for reliability evidence within a bounded time range.",
		runtimeExecutor[logs.Input](logsRuntime),
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", logs.Name, err)
	}

	metricsRuntime := metrics.NewMockTool(config.MetricsTimeout).Runtime()
	if strings.EqualFold(config.MetricsBackend, "prometheus") {
		metricsRuntime = metrics.NewSearchTool(config.MetricsSearcher, metrics.SearchToolConfig{
			Backend:        config.MetricsBackend,
			BaseURL:        config.MetricsBaseURL,
			FallbackToMock: config.MetricsFallbackToMock,
			Timeout:        config.MetricsTimeout,
		}).Runtime()
	}
	metricsTool, err := toolutils.InferTool(
		metrics.Name,
		"Query service metrics for latency, error-rate, or symptom evidence.",
		runtimeExecutor[metrics.Input](metricsRuntime),
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", metrics.Name, err)
	}

	alertsRuntime := alerts.NewMockTool(config.AlertsTimeout).Runtime()
	alertsBackend := config.AlertsBackend
	if alertsBackend == "" {
		alertsBackend = config.MetricsBackend
	}
	alertsFallbackToMock := config.AlertsFallbackToMock
	if !alertsFallbackToMock {
		alertsFallbackToMock = config.MetricsFallbackToMock
	}
	if strings.EqualFold(alertsBackend, "prometheus") {
		alertsRuntime = alerts.NewSearchTool(config.AlertsStore, alerts.SearchToolConfig{
			Backend:        alertsBackend,
			BaseURL:        config.AlertsBaseURL,
			FallbackToMock: alertsFallbackToMock,
			Timeout:        config.AlertsTimeout,
		}).Runtime()
	}
	alertsTool, err := toolutils.InferTool(
		alerts.Name,
		"Query active or recent alert signals for a service.",
		runtimeExecutor[alerts.Input](alertsRuntime),
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", alerts.Name, err)
	}

	tracesRuntime := traces.NewMockTool(config.TracesTimeout).Runtime()
	if strings.EqualFold(config.TracesBackend, "jaeger") {
		tracesRuntime = traces.NewSearchTool(config.TracesSearcher, traces.SearchToolConfig{
			Backend:        config.TracesBackend,
			BaseURL:        config.TracesBaseURL,
			DefaultService: config.TracesDefaultService,
			DefaultLimit:   config.TracesDefaultLimit,
			FallbackToMock: config.TracesFallbackToMock,
			Timeout:        config.TracesTimeout,
		}).Runtime()
	}
	tracesTool, err := toolutils.InferTool(
		traces.Name,
		"Query distributed traces for slow spans and request-path evidence.",
		runtimeExecutor[traces.Input](tracesRuntime),
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", traces.Name, err)
	}

	knowledgeRuntime := knowledge.NewMockTool(config.KnowledgeTimeout).Runtime()
	if config.KnowledgeSearcher != nil {
		knowledgeRuntime = knowledge.NewSearchTool(
			config.KnowledgeSearcher,
			config.KnowledgeTimeout,
		).Runtime()
	}
	knowledgeTool, err := toolutils.InferTool(
		knowledge.Name,
		"Search operational knowledge and runbooks for relevant evidence.",
		runtimeExecutor[knowledge.Input](knowledgeRuntime),
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", knowledge.Name, err)
	}

	topologyRuntime := topology.NewMockTool(config.TopologyTimeout).Runtime()
	topologyTool, err := toolutils.InferTool(
		topology.Name,
		"Get service dependency topology context for an on-call investigation.",
		runtimeExecutor[topology.Input](topologyRuntime),
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", topology.Name, err)
	}

	return []einotool.InvokableTool{
		logsTool,
		metricsTool,
		alertsTool,
		tracesTool,
		knowledgeTool,
		topologyTool,
	}, nil
}

func runtimeExecutor[Input any](
	toolRuntime *toolruntime.Runtime,
) func(context.Context, Input) (common.ToolResult, error) {
	return func(ctx context.Context, input Input) (common.ToolResult, error) {
		return common.ExecuteRuntime(ctx, toolRuntime, input)
	}
}
