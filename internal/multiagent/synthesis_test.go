package multiagent

import (
	"context"
	"errors"
	"testing"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type synthesisStub struct {
	output agenteino.AgentOutput
	err    error
}

func (s synthesisStub) Synthesize(
	context.Context,
	SynthesisInput,
) (agenteino.AgentOutput, error) {
	return s.output, s.err
}

func TestDeterministicSynthesizerProducesEvidenceBoundChineseAnswer(t *testing.T) {
	input := synthesisFixture("zh")
	output, err := (DeterministicSynthesizer{}).Synthesize(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if len(output.Conclusions) != 2 ||
		len(output.Inferences) != 1 ||
		len(output.Recommendations) != 1 {
		t.Fatalf("output = %#v", output)
	}
	if err := validateSynthesisOutput(output, input.Evidence); err != nil {
		t.Fatalf("validateSynthesisOutput() error = %v", err)
	}
	if output.Conclusions[0].Text == "" ||
		output.Conclusions[0].Text[0] < 0x80 {
		t.Fatalf("expected Chinese conclusion, got %q", output.Conclusions[0].Text)
	}
}

func TestSynthesisAgentFallsBackOnPrimaryFailure(t *testing.T) {
	input := synthesisFixture("en")
	agent := NewSynthesisAgent(synthesisStub{err: errors.New("model unavailable")})
	output, err := agent.Synthesize(context.Background(), input)
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if output.Metadata["fallback_used"] != true ||
		output.Metadata["fallback_reason"] != "primary_synthesis_failed" {
		t.Fatalf("Metadata = %#v", output.Metadata)
	}
	if len(output.Conclusions) == 0 {
		t.Fatal("deterministic fallback returned no conclusion")
	}
}

func TestSynthesisAgentRejectsInventedEvidenceID(t *testing.T) {
	input := synthesisFixture("en")
	agent := NewSynthesisAgent(synthesisStub{output: agenteino.AgentOutput{
		Conclusions: []agenteino.Conclusion{{
			Text:        "Unsupported claim",
			EvidenceIDs: []string{"invented-evidence"},
		}},
	}})
	output, err := agent.Synthesize(context.Background(), input)
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if output.Metadata["fallback_reason"] != "invalid_evidence_reference" {
		t.Fatalf("Metadata = %#v", output.Metadata)
	}
	for _, conclusion := range output.Conclusions {
		for _, id := range conclusion.EvidenceIDs {
			if id == "invented-evidence" {
				t.Fatal("invented evidence survived fallback")
			}
		}
	}
}

func TestDeterministicSynthesizerReportsEmptyEvidence(t *testing.T) {
	output, err := (DeterministicSynthesizer{}).Synthesize(
		context.Background(),
		SynthesisInput{Plan: TriagePlan{Language: "en"}},
	)
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if len(output.Conclusions) != 0 ||
		len(output.Limitations) != 1 ||
		output.Limitations[0].Code != "MULTI_AGENT_EVIDENCE_EMPTY" {
		t.Fatalf("output = %#v", output)
	}
}

func synthesisFixture(language string) SynthesisInput {
	evidence := []common.EvidenceItem{
		{
			ID:         "metric-1",
			SourceType: "metrics",
			Content:    "checkout error rate is elevated",
		},
		{
			ID:         "runbook-1",
			SourceType: "knowledge",
			Content:    "checkout runbook",
		},
	}
	return SynthesisInput{
		Plan: TriagePlan{
			Service:      "checkout",
			IncidentType: IncidentHighErrorRate,
			Language:     language,
		},
		EvidenceFinding: AgentFinding{
			Role:        AgentRoleEvidence,
			EvidenceIDs: []string{"metric-1"},
		},
		KnowledgeFinding: AgentFinding{
			Role:        AgentRoleKnowledge,
			EvidenceIDs: []string{"runbook-1"},
		},
		Evidence: evidence,
	}
}
