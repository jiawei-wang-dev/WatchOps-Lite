package processor

import (
	"context"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

func (p *Processor) Process(
	ctx context.Context,
	items []evidence.Item,
) EvidenceReport {
	if p == nil {
		p = NewDefault()
	}
	ctx, span := observability.StartSpan(
		ctx,
		"evidence.process",
		attribute.Int("evidence.original_count", len(items)),
	)
	defer span.End()

	deduped := p.dedupe(ctx, items)
	scored := p.score(ctx, deduped)
	sortProcessed(scored)
	p.assignCitations(ctx, scored)
	groups := p.group(ctx, scored)

	metadata := map[string]any{
		"evidence_original_count": len(items),
		"evidence_deduped_count":  len(scored),
		"evidence_group_count":    len(groups),
		"citation_enabled":        true,
	}
	span.SetAttributes(
		attribute.Int("evidence.deduped_count", len(scored)),
		attribute.Int("evidence.group_count", len(groups)),
	)
	return EvidenceReport{
		Items:    scored,
		Groups:   groups,
		Metadata: metadata,
	}
}
