package diagnosis

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	evidenceprocessor "github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence/processor"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type Evaluator struct{}

func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

func (e *Evaluator) Evaluate(
	ctx context.Context,
	set HypothesisSet,
	report evidenceprocessor.EvidenceReport,
) HypothesisSet {
	ctx, span := observability.StartSpan(
		ctx,
		"diagnosis.hypothesis.evaluate",
		attribute.Int("hypothesis.count", len(set.Items)),
		attribute.Int("evidence.count", len(report.Items)),
	)
	defer span.End()

	items := make([]Hypothesis, 0, len(set.Items))
	for _, hypothesis := range set.Items {
		evaluated := evaluateOne(hypothesis, report.Items)
		items = append(items, evaluated)
	}
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].Score != items[right].Score {
			return items[left].Score > items[right].Score
		}
		return items[left].ID < items[right].ID
	})
	metadata := cloneMetadata(set.Metadata)
	metadata["hypothesis_evaluated"] = true
	metadata["evidence_count"] = len(report.Items)
	span.SetAttributes(attribute.Int("supported_hypothesis_count", supportedCount(items)))
	return HypothesisSet{
		Items:    items,
		Source:   set.Source,
		Metadata: metadata,
	}
}

func evaluateOne(
	hypothesis Hypothesis,
	evidence []evidenceprocessor.ProcessedEvidence,
) Hypothesis {
	support := []string{}
	missing := []string{}
	for _, expected := range hypothesis.ExpectedEvidence {
		if citation := firstSupportingCitation(expected, evidence); citation != "" {
			support = appendUnique(support, citation)
			continue
		}
		missing = append(missing, expected)
	}
	hypothesis.SupportingEvidence = support
	hypothesis.MissingEvidence = missing
	switch {
	case len(support) >= 2:
		hypothesis.Status = StatusSupported
	case len(support) == 0:
		hypothesis.Status = StatusWeak
	default:
		hypothesis.Status = StatusProposed
	}
	hypothesis.Score = round(
		math.Min(1, hypothesis.Confidence+float64(len(support))*0.12),
	)
	return hypothesis
}

func firstSupportingCitation(
	expected string,
	evidence []evidenceprocessor.ProcessedEvidence,
) string {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return ""
	}
	for _, item := range evidence {
		haystack := strings.ToLower(strings.Join([]string{
			item.Content,
			item.Title,
			item.ResourceID,
			item.TraceID,
			fmt.Sprint(item.Metadata),
		}, " "))
		if strings.Contains(haystack, expected) {
			return item.CitationID
		}
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func supportedCount(items []Hypothesis) int {
	total := 0
	for _, item := range items {
		if item.Status == StatusSupported {
			total++
		}
	}
	return total
}

func cloneMetadata(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func round(value float64) float64 {
	return math.Round(value*1000) / 1000
}
