package diagnosis

import (
	"context"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type RuleBasedHypothesisGenerator struct{}

func NewRuleBasedHypothesisGenerator() *RuleBasedHypothesisGenerator {
	return &RuleBasedHypothesisGenerator{}
}

func (g *RuleBasedHypothesisGenerator) Generate(
	ctx context.Context,
	input GenerateInput,
) (HypothesisSet, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"diagnosis.hypothesis.generate",
		attribute.String("intent.type", string(input.Intent.Intent)),
	)
	defer span.End()

	text := strings.ToLower(strings.Join(append(
		[]string{input.Message, input.Symptom},
		input.Keywords...,
	), " "))
	items := []Hypothesis{}
	switch {
	case input.Intent.Intent == intent.IntentTraceAnalysis:
		items = traceHypotheses()
	case containsAny(text, "latency", "slow", "p95", "timeout", "超时", "慢"):
		items = latencyHypotheses()
	case containsAny(text, "error", "5xx", "500", "fail", "失败", "报错", "异常"):
		items = errorHypotheses()
	case input.Intent.Intent == intent.IntentIncidentTriage:
		items = errorHypotheses()
	}
	for index := range items {
		items[index].Status = StatusProposed
		items[index].Score = items[index].Confidence
	}
	span.SetAttributes(attribute.Int("hypothesis.count", len(items)))
	return HypothesisSet{
		Items:  items,
		Source: "rule",
		Metadata: map[string]any{
			"hypothesis_enabled": len(items) > 0,
			"intent_type":        string(input.Intent.Intent),
		},
	}, nil
}

func errorHypotheses() []Hypothesis {
	return []Hypothesis{
		{
			ID:          "H1",
			Title:       "upstream dependency timeout",
			Description: "Checkout requests may be failing because an upstream dependency is timing out.",
			ExpectedEvidence: []string{
				"timeout", "deadline", "upstream", "dependency", "retry",
			},
			SuggestedTools: []intent.ToolName{
				intent.ToolQueryMetrics,
				intent.ToolQueryLogs,
				intent.ToolQueryTraces,
			},
			Confidence: 0.72,
		},
		{
			ID:          "H2",
			Title:       "database failure",
			Description: "Database errors or connection exhaustion may be contributing to 5xx responses.",
			ExpectedEvidence: []string{
				"database", "mysql", "connection", "pool", "db",
			},
			SuggestedTools: []intent.ToolName{
				intent.ToolQueryLogs,
				intent.ToolQueryMetrics,
			},
			Confidence: 0.58,
		},
		{
			ID:          "H3",
			Title:       "deployment regression",
			Description: "A recent deployment or configuration change may have introduced the error spike.",
			ExpectedEvidence: []string{
				"deployment", "release", "rollback", "config", "version",
			},
			SuggestedTools: []intent.ToolName{
				intent.ToolQueryLogs,
				intent.ToolSearchKnowledge,
			},
			Confidence: 0.5,
		},
		{
			ID:          "H4",
			Title:       "traffic spike or overload",
			Description: "A sudden traffic increase may be overloading checkout or a dependency.",
			ExpectedEvidence: []string{
				"traffic", "qps", "saturation", "overload", "rate",
			},
			SuggestedTools: []intent.ToolName{
				intent.ToolQueryMetrics,
				intent.ToolQueryLogs,
			},
			Confidence: 0.48,
		},
	}
}

func latencyHypotheses() []Hypothesis {
	return []Hypothesis{
		{
			ID:               "H1",
			Title:            "slow downstream dependency",
			Description:      "Latency may be caused by a slow downstream dependency.",
			ExpectedEvidence: []string{"latency", "slow", "downstream", "dependency", "timeout"},
			SuggestedTools:   []intent.ToolName{intent.ToolQueryMetrics, intent.ToolQueryTraces},
			Confidence:       0.72,
		},
		{
			ID:               "H2",
			Title:            "database bottleneck",
			Description:      "Database calls may be dominating request latency.",
			ExpectedEvidence: []string{"database", "mysql", "query", "duration", "pool"},
			SuggestedTools:   []intent.ToolName{intent.ToolQueryTraces, intent.ToolQueryLogs},
			Confidence:       0.6,
		},
		{
			ID:               "H3",
			Title:            "connection pool exhaustion",
			Description:      "Connection pool exhaustion may be queuing requests.",
			ExpectedEvidence: []string{"connection", "pool", "exhaustion", "queue", "saturation"},
			SuggestedTools:   []intent.ToolName{intent.ToolQueryMetrics, intent.ToolQueryLogs},
			Confidence:       0.55,
		},
		{
			ID:               "H4",
			Title:            "external API latency",
			Description:      "An external API may be slow and increasing end-to-end latency.",
			ExpectedEvidence: []string{"external", "api", "latency", "dependency", "span"},
			SuggestedTools:   []intent.ToolName{intent.ToolQueryTraces, intent.ToolQueryLogs},
			Confidence:       0.52,
		},
	}
}

func traceHypotheses() []Hypothesis {
	return []Hypothesis{
		{
			ID:               "H1",
			Title:            "slow span dependency",
			Description:      "The trace may contain a slow dependency span on the critical path.",
			ExpectedEvidence: []string{"slow", "span", "dependency", "critical path", "duration"},
			SuggestedTools:   []intent.ToolName{intent.ToolQueryTraces},
			Confidence:       0.76,
		},
		{
			ID:               "H2",
			Title:            "external API latency",
			Description:      "A downstream external API may dominate trace duration.",
			ExpectedEvidence: []string{"external", "api", "latency", "span", "downstream"},
			SuggestedTools:   []intent.ToolName{intent.ToolQueryTraces, intent.ToolQueryLogs},
			Confidence:       0.62,
		},
		{
			ID:               "H3",
			Title:            "lock or contention",
			Description:      "Lock contention or queueing may explain slow spans.",
			ExpectedEvidence: []string{"lock", "contention", "queue", "wait", "blocked"},
			SuggestedTools:   []intent.ToolName{intent.ToolQueryTraces, intent.ToolQueryLogs},
			Confidence:       0.46,
		},
		{
			ID:               "H4",
			Title:            "serialization or network overhead",
			Description:      "Serialization or network overhead may inflate span duration.",
			ExpectedEvidence: []string{"serialization", "network", "payload", "io", "duration"},
			SuggestedTools:   []intent.ToolName{intent.ToolQueryTraces},
			Confidence:       0.42,
		},
	}
}

func containsAny(value string, candidates ...string) bool {
	value = strings.ToLower(value)
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}
