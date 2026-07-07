package processor

import (
	"context"
	"fmt"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
)

func (p *Processor) assignCitations(
	ctx context.Context,
	items []ProcessedEvidence,
) {
	_, span := observability.StartSpan(ctx, "evidence.citation")
	defer span.End()

	prefix := p.config.CitationPrefix
	for index := range items {
		citationID := fmt.Sprintf("%s%d", prefix, index+1)
		items[index].CitationID = citationID
		if items[index].Metadata == nil {
			items[index].Metadata = map[string]any{}
		}
		items[index].Metadata["citation_id"] = citationID
	}
}
