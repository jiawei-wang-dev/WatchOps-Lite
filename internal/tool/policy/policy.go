package policy

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const (
	ToolLogs      = "query_logs"
	ToolMetrics   = "query_metrics"
	ToolTraces    = "query_traces"
	ToolKnowledge = "search_knowledge"
)

type QueryType string

const (
	QueryUnknown   QueryType = "unknown"
	QueryLogs      QueryType = "logs"
	QueryMetrics   QueryType = "metrics"
	QueryTraces    QueryType = "traces"
	QueryKnowledge QueryType = "knowledge"
	QueryMixed     QueryType = "mixed"
)

type ToolStats struct {
	HistoricalLatencyMS float64
	SuccessRate         float64
	FallbackFrequency   float64
	RelativeCost        float64
}

type AgentContext struct {
	Service            string
	Stats              map[string]ToolStats
	RealSourceFailures map[string]bool
}

type Request struct {
	Query   string
	Context AgentContext
}

type RankedTool struct {
	Name              string
	Source            evidence.Source
	Score             float64
	Relevance         float64
	ExpectedLatencyMS float64
	SuccessRate       float64
	FallbackFrequency float64
	RelativeCost      float64
}

type Step struct {
	Position int
	Tool     string
	Source   evidence.Source
	Reason   string
}

type FallbackDecision struct {
	AllowMockFallback bool
	Condition         string
	FailedRealSources []string
}

type Plan struct {
	QueryType QueryType
	Steps     []Step
	Fallback  FallbackDecision
}

type Policy struct {
	defaultStats map[string]ToolStats
}

func New() *Policy {
	return &Policy{defaultStats: map[string]ToolStats{
		ToolMetrics: {
			HistoricalLatencyMS: 80,
			SuccessRate:         0.98,
			FallbackFrequency:   0.03,
			RelativeCost:        0.10,
		},
		ToolLogs: {
			HistoricalLatencyMS: 150,
			SuccessRate:         0.97,
			FallbackFrequency:   0.05,
			RelativeCost:        0.20,
		},
		ToolKnowledge: {
			HistoricalLatencyMS: 180,
			SuccessRate:         0.98,
			FallbackFrequency:   0.04,
			RelativeCost:        0.30,
		},
		ToolTraces: {
			HistoricalLatencyMS: 300,
			SuccessRate:         0.95,
			FallbackFrequency:   0.08,
			RelativeCost:        0.60,
		},
	}}
}

func (p *Policy) Rank(ctx context.Context, request Request) []RankedTool {
	queryType, relevance := classify(request.Query)
	ctx, span := observability.StartSpan(
		ctx,
		"tool.policy.rank",
		attribute.String("query_type", string(queryType)),
	)
	defer span.End()

	ranked := make([]RankedTool, 0, len(relevance))
	for toolName, relevanceScore := range relevance {
		if relevanceScore < 0.5 {
			continue
		}
		stats := p.statsFor(toolName, request.Context.Stats)
		ranked = append(ranked, RankedTool{
			Name:              toolName,
			Source:            sourceForTool(toolName),
			Score:             rankScore(relevanceScore, stats),
			Relevance:         relevanceScore,
			ExpectedLatencyMS: stats.HistoricalLatencyMS,
			SuccessRate:       stats.SuccessRate,
			FallbackFrequency: stats.FallbackFrequency,
			RelativeCost:      stats.RelativeCost,
		})
	}
	sort.SliceStable(ranked, func(left, right int) bool {
		if ranked[left].Score != ranked[right].Score {
			return ranked[left].Score > ranked[right].Score
		}
		if ranked[left].ExpectedLatencyMS != ranked[right].ExpectedLatencyMS {
			return ranked[left].ExpectedLatencyMS < ranked[right].ExpectedLatencyMS
		}
		return ranked[left].Name < ranked[right].Name
	})
	span.SetAttributes(attribute.StringSlice("tool_selection_order", rankedNames(ranked)))
	return ranked
}

func (p *Policy) Plan(ctx context.Context, request Request) Plan {
	ranked := p.Rank(ctx, request)
	queryType, _ := classify(request.Query)
	steps := make([]Step, 0, len(ranked))
	for index, item := range ranked {
		steps = append(steps, Step{
			Position: index + 1,
			Tool:     item.Name,
			Source:   item.Source,
			Reason:   stepReason(item),
		})
	}
	fallback := fallbackDecision(steps, request.Context.RealSourceFailures)
	_, span := observability.StartSpan(
		ctx,
		"tool.policy.plan",
		attribute.String("query_type", string(queryType)),
		attribute.StringSlice("tool_selection_order", stepNames(steps)),
		attribute.String("plan_structure", lightweightPlan(queryType, steps, fallback)),
	)
	span.End()
	return Plan{
		QueryType: queryType,
		Steps:     steps,
		Fallback:  fallback,
	}
}

func (p *Policy) statsFor(name string, overrides map[string]ToolStats) ToolStats {
	stats := p.defaultStats[name]
	if override, ok := overrides[name]; ok {
		if override.HistoricalLatencyMS > 0 {
			stats.HistoricalLatencyMS = override.HistoricalLatencyMS
		}
		if override.SuccessRate >= 0 && override.SuccessRate <= 1 {
			stats.SuccessRate = override.SuccessRate
		}
		if override.FallbackFrequency >= 0 && override.FallbackFrequency <= 1 {
			stats.FallbackFrequency = override.FallbackFrequency
		}
		if override.RelativeCost > 0 {
			stats.RelativeCost = override.RelativeCost
		}
	}
	return stats
}

func rankScore(relevance float64, stats ToolStats) float64 {
	latencyPenalty := math.Min(stats.HistoricalLatencyMS/1000, 1)
	return relevance*0.55 +
		stats.SuccessRate*0.25 -
		latencyPenalty*0.10 -
		stats.FallbackFrequency*0.05 -
		stats.RelativeCost*0.05
}

func classify(query string) (QueryType, map[string]float64) {
	query = strings.ToLower(strings.TrimSpace(query))
	relevance := map[string]float64{}
	hasMetrics := containsAny(query, "metric", "error rate", "latency", "p95", "p99", "slo")
	hasLogs := containsAny(query, "log", "error", "timeout", "exception", "deadline")
	hasTraces := containsAny(query, "trace", "span", "critical path", "slow request")
	hasKnowledge := containsAny(query, "runbook", "playbook", "procedure", "how should", "mitigation")

	if hasMetrics {
		relevance[ToolMetrics] = 1
		relevance[ToolLogs] = max(relevance[ToolLogs], 0.72)
	}
	if hasLogs {
		relevance[ToolLogs] = 1
		relevance[ToolMetrics] = max(relevance[ToolMetrics], 0.68)
	}
	if hasTraces {
		relevance[ToolTraces] = 1
		relevance[ToolLogs] = max(relevance[ToolLogs], 0.62)
		relevance[ToolMetrics] = max(relevance[ToolMetrics], 0.55)
	}
	if hasKnowledge {
		relevance[ToolKnowledge] = 1
	}

	count := 0
	var queryType QueryType
	for _, matched := range []struct {
		ok    bool
		value QueryType
	}{
		{hasMetrics, QueryMetrics},
		{hasLogs, QueryLogs},
		{hasTraces, QueryTraces},
		{hasKnowledge, QueryKnowledge},
	} {
		if matched.ok {
			count++
			queryType = matched.value
		}
	}
	if count == 0 {
		return QueryUnknown, relevance
	}
	if count > 1 {
		return QueryMixed, relevance
	}
	return queryType, relevance
}

func fallbackDecision(steps []Step, failures map[string]bool) FallbackDecision {
	decision := FallbackDecision{
		Condition:         "all_real_sources_failed",
		FailedRealSources: []string{},
	}
	if len(steps) == 0 {
		return decision
	}
	for _, step := range steps {
		if !failures[step.Tool] {
			return decision
		}
		decision.FailedRealSources = append(decision.FailedRealSources, step.Tool)
	}
	decision.AllowMockFallback = true
	return decision
}

func sourceForTool(name string) evidence.Source {
	switch name {
	case ToolLogs:
		return evidence.SourceLogs
	case ToolMetrics:
		return evidence.SourceMetrics
	case ToolTraces:
		return evidence.SourceTraces
	case ToolKnowledge:
		return evidence.SourceKnowledge
	default:
		return ""
	}
}

func stepReason(tool RankedTool) string {
	return "ranked by relevance, historical reliability, latency, fallback frequency, and relative cost"
}

func rankedNames(values []RankedTool) []string {
	result := make([]string, 0, len(values))
	for _, item := range values {
		result = append(result, item.Name)
	}
	return result
}

func stepNames(values []Step) []string {
	result := make([]string, 0, len(values))
	for _, item := range values {
		result = append(result, item.Tool)
	}
	return result
}

func lightweightPlan(queryType QueryType, steps []Step, fallback FallbackDecision) string {
	return "type=" + string(queryType) +
		";tools=" + strings.Join(stepNames(steps), ",") +
		";mock_fallback=" + fallback.Condition
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func max(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
