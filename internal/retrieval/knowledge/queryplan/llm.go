package queryplan

import (
	"context"
	"errors"
)

type LLMPlanner struct{}

func NewLLMPlanner() *LLMPlanner {
	return &LLMPlanner{}
}

func (p *LLMPlanner) Plan(
	context.Context,
	QueryPlanInput,
) (RAGQueryPlan, error) {
	return RAGQueryPlan{}, errors.New("LLM query planning is not configured")
}
