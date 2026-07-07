package diagnosis

import (
	"context"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	evidenceprocessor "github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence/processor"
)

func TestEvaluatorBindsEvidenceCitationsToHypotheses(t *testing.T) {
	report := evidenceprocessor.NewDefault().Process(context.Background(), []evidence.Item{
		{
			ID:      "log-1",
			Source:  evidence.SourceLogs,
			Content: "checkout upstream dependency timeout with retry amplification",
			Metadata: map[string]any{
				"log_id": "log-1",
				"level":  "error",
			},
		},
		{
			ID:      "metric-1",
			Source:  evidence.SourceMetrics,
			Content: "dependency error rate increased for checkout",
			Metadata: map[string]any{
				"metric_name": "dependency_error_rate",
				"value":       0.08,
			},
		},
	})
	set := HypothesisSet{Items: []Hypothesis{{
		ID:               "H1",
		Title:            "upstream dependency timeout",
		ExpectedEvidence: []string{"upstream", "dependency", "timeout"},
		Confidence:       0.7,
	}}}

	evaluated := NewEvaluator().Evaluate(context.Background(), set, report)

	if len(evaluated.Items) != 1 ||
		evaluated.Items[0].Status != StatusSupported ||
		len(evaluated.Items[0].SupportingEvidence) == 0 {
		t.Fatalf("evaluated = %#v", evaluated.Items)
	}
}

func TestEvaluatorMarksHypothesisWeakWithoutEvidence(t *testing.T) {
	set := HypothesisSet{Items: []Hypothesis{{
		ID:               "H1",
		Title:            "database failure",
		ExpectedEvidence: []string{"database", "mysql"},
		Confidence:       0.6,
	}}}

	evaluated := NewEvaluator().Evaluate(
		context.Background(),
		set,
		evidenceprocessor.EvidenceReport{},
	)

	if len(evaluated.Items) != 1 ||
		evaluated.Items[0].Status != StatusWeak ||
		len(evaluated.Items[0].MissingEvidence) != 2 {
		t.Fatalf("evaluated = %#v", evaluated.Items)
	}
}
