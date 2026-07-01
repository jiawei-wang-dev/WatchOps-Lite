package eino

import (
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
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
	TracesTimeout         time.Duration
	KnowledgeTimeout      time.Duration
	KnowledgeSearcher     knowledge.Searcher
}

func BuildMockTools() ([]einotool.InvokableTool, error) {
	return BuildMockToolsWithConfig(MockToolsConfig{})
}

func BuildMockToolsWithConfig(config MockToolsConfig) ([]einotool.InvokableTool, error) {
	logsExecutor := logs.NewMockTool(config.LogsTimeout).Execute
	if strings.EqualFold(config.LogsBackend, "elasticsearch") {
		logsExecutor = logs.NewSearchTool(config.LogsSearcher, logs.SearchToolConfig{
			Backend:        config.LogsBackend,
			Index:          config.LogsIndex,
			DefaultLimit:   config.LogsDefaultLimit,
			FallbackToMock: config.LogsFallbackToMock,
			Timeout:        config.LogsTimeout,
		}).Execute
	}
	logsTool, err := toolutils.InferTool(
		logs.Name,
		"Query service logs for reliability evidence within a bounded time range.",
		logsExecutor,
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", logs.Name, err)
	}

	metricsExecutor := metrics.NewMockTool(config.MetricsTimeout).Execute
	if strings.EqualFold(config.MetricsBackend, "prometheus") {
		metricsExecutor = metrics.NewSearchTool(config.MetricsSearcher, metrics.SearchToolConfig{
			Backend:        config.MetricsBackend,
			BaseURL:        config.MetricsBaseURL,
			FallbackToMock: config.MetricsFallbackToMock,
			Timeout:        config.MetricsTimeout,
		}).Execute
	}
	metricsTool, err := toolutils.InferTool(
		metrics.Name,
		"Query service metrics for latency, error-rate, or symptom evidence.",
		metricsExecutor,
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", metrics.Name, err)
	}

	tracesTool, err := toolutils.InferTool(
		traces.Name,
		"Query distributed traces for slow spans and request-path evidence.",
		traces.NewMockTool(config.TracesTimeout).Execute,
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", traces.Name, err)
	}

	knowledgeExecutor := knowledge.NewMockTool(config.KnowledgeTimeout).Execute
	if config.KnowledgeSearcher != nil {
		knowledgeExecutor = knowledge.NewSearchTool(
			config.KnowledgeSearcher,
			config.KnowledgeTimeout,
		).Execute
	}
	knowledgeTool, err := toolutils.InferTool(
		knowledge.Name,
		"Search operational knowledge and runbooks for relevant evidence.",
		knowledgeExecutor,
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", knowledge.Name, err)
	}

	return []einotool.InvokableTool{
		logsTool,
		metricsTool,
		tracesTool,
		knowledgeTool,
	}, nil
}
