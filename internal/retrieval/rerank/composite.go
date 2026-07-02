package rerank

import (
	"context"
	"errors"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type CompositeReranker struct {
	external Reranker
	fallback Reranker
}

func NewComposite(external, fallback Reranker) (*CompositeReranker, error) {
	if fallback == nil {
		return nil, ErrInvalidArgument
	}
	return &CompositeReranker{external: external, fallback: fallback}, nil
}

func (r *CompositeReranker) Rerank(
	ctx context.Context,
	query string,
	candidates []Candidate,
	topK int,
) ([]Result, error) {
	if r.external != nil {
		results, err := r.external.Rerank(ctx, query, candidates, topK)
		if err == nil && len(results) > 0 {
			return results, nil
		}
		reason := fallbackReason(err)
		ctx, span := observability.StartSpan(
			ctx,
			"retrieval.rerank.fallback",
			attribute.Int("candidate_count", len(candidates)),
			attribute.Int("top_k", topK),
			attribute.Bool("fallback_used", true),
			attribute.String("fallback_reason", reason),
		)
		results, fallbackErr := r.fallback.Rerank(ctx, query, candidates, topK)
		if fallbackErr != nil {
			observability.MarkError(span, "rerank fallback failed")
			span.End()
			return nil, fallbackErr
		}
		for index := range results {
			results[index].FallbackReason = reason
		}
		span.End()
		return results, nil
	}
	results, err := r.fallback.Rerank(ctx, query, candidates, topK)
	if err != nil {
		return nil, err
	}
	for index := range results {
		results[index].FallbackReason = "external_not_configured"
	}
	return results, nil
}

func fallbackReason(err error) string {
	switch {
	case err == nil, errors.Is(err, ErrEmptyResult):
		return "external_empty_result"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "external_timeout"
	case errors.Is(err, ErrInvalidResponse):
		return "external_invalid_response"
	default:
		return "external_unavailable"
	}
}

var _ Reranker = (*CompositeReranker)(nil)
