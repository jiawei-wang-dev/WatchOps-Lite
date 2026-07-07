package diagnosis

import (
	"context"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

const (
	StatusProposed  = "proposed"
	StatusSupported = "supported"
	StatusWeak      = "weak"
	StatusRejected  = "rejected"
)

type Hypothesis struct {
	ID                 string            `json:"id"`
	Title              string            `json:"title"`
	Description        string            `json:"description"`
	ExpectedEvidence   []string          `json:"expected_evidence"`
	SuggestedTools     []intent.ToolName `json:"suggested_tools"`
	Confidence         float64           `json:"confidence"`
	Status             string            `json:"status"`
	SupportingEvidence []string          `json:"supporting_evidence,omitempty"`
	MissingEvidence    []string          `json:"missing_evidence,omitempty"`
	Score              float64           `json:"score"`
}

type HypothesisSet struct {
	Items    []Hypothesis   `json:"items"`
	Source   string         `json:"source"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type GenerateInput struct {
	Intent   intent.IntentResult
	Message  string
	Service  string
	Symptom  string
	Keywords []string
}

type Generator interface {
	Generate(ctx context.Context, input GenerateInput) (HypothesisSet, error)
}
