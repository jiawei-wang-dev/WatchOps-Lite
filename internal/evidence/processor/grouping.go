package processor

import (
	"context"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
)

func (p *Processor) group(
	ctx context.Context,
	items []ProcessedEvidence,
) []EvidenceGroup {
	_ = ctx
	grouped := map[evidence.Source][]ProcessedEvidence{}
	for _, item := range items {
		grouped[item.Source] = append(grouped[item.Source], item)
	}
	order := []evidence.Source{
		evidence.SourceMetrics,
		evidence.SourceLogs,
		evidence.SourceTraces,
		evidence.SourceKnowledge,
		evidence.SourceAlerts,
		evidence.SourceTopology,
	}
	groups := make([]EvidenceGroup, 0, len(grouped))
	for _, source := range order {
		if len(grouped[source]) == 0 {
			continue
		}
		groups = append(groups, EvidenceGroup{
			Source: source,
			Items:  grouped[source],
		})
	}
	return groups
}
