package queryplan

import (
	"context"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type HybridPlanner struct {
	llm  QueryPlanner
	rule QueryPlanner
}

func NewHybridPlanner(llm QueryPlanner, rule QueryPlanner) *HybridPlanner {
	if rule == nil {
		rule = NewRuleBasedPlanner()
	}
	return &HybridPlanner{llm: llm, rule: rule}
}

func (p *HybridPlanner) Plan(
	ctx context.Context,
	input QueryPlanInput,
) (RAGQueryPlan, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"rag.query_plan",
		attribute.String("intent.type", string(input.Intent.Intent)),
	)
	defer span.End()

	if p.llm != nil {
		plan, err := p.llm.Plan(ctx, input)
		if err == nil && len(plan.Queries) > 0 {
			plan.Source = "llm"
			plan.Queries = normalizeQueries(plan.Queries)
			if plan.Metadata == nil {
				plan.Metadata = map[string]any{}
			}
			plan.Metadata["query_plan_fallback_used"] = false
			span.SetAttributes(
				attribute.String("rag.query_plan_source", plan.Source),
				attribute.Int("rag.sub_query_count", len(plan.Queries)),
			)
			return plan, nil
		}
	}
	plan, err := p.rule.Plan(ctx, input)
	if err != nil {
		return RAGQueryPlan{}, err
	}
	plan.Source = "rule"
	if plan.Metadata == nil {
		plan.Metadata = map[string]any{}
	}
	plan.Metadata["query_plan_fallback_used"] = p.llm != nil
	span.SetAttributes(
		attribute.String("rag.query_plan_source", plan.Source),
		attribute.Int("rag.sub_query_count", len(plan.Queries)),
		attribute.Bool("rag.query_plan_fallback_used", p.llm != nil),
	)
	return plan, nil
}
