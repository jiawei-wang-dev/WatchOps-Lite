package eino

import (
	"fmt"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/traces"
)

type MockToolsConfig struct {
	LogsTimeout       time.Duration
	MetricsTimeout    time.Duration
	TracesTimeout     time.Duration
	KnowledgeTimeout  time.Duration
	KnowledgeSearcher knowledge.Searcher
}

func BuildMockTools() ([]einotool.InvokableTool, error) {
	return BuildMockToolsWithConfig(MockToolsConfig{})
}

func BuildMockToolsWithConfig(config MockToolsConfig) ([]einotool.InvokableTool, error) {
	logsTool, err := toolutils.InferTool(
		logs.Name,
		"Query service logs for reliability evidence within a bounded time range.",
		logs.NewMockTool(config.LogsTimeout).Execute,
	)
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", logs.Name, err)
	}

	metricsTool, err := toolutils.InferTool(
		metrics.Name,
		"Query service metrics for latency, error-rate, or symptom evidence.",
		metrics.NewMockTool(config.MetricsTimeout).Execute,
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
