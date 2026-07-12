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

func TestFinalDiagnosisKeepsSymptomFindingOutOfRootCause(t *testing.T) {
	input := synthesisFixture("zh")
	input.Evidence = append(input.Evidence, common.EvidenceItem{
		ID:         "memory-1",
		SourceType: "memory",
		Content:    "历史故障显示 payment 延迟曾导致 checkout 错误率升高。",
	})
	output := agenteino.AgentOutput{
		Conclusions: []agenteino.Conclusion{{
			Text:        "checkout 服务错误率为 6.2%，处于异常高位。",
			EvidenceIDs: []string{"metric-1"},
		}, {
			Text:        "历史故障模式与 payment 延迟导致 checkout 错误率升高相似。",
			EvidenceIDs: []string{"memory-1"},
		}},
		Inferences: []agenteino.Inference{{
			Text:        "payment 依赖延迟或超时可能导致 checkout 错误率升高，但缺少日志和 Trace 无法确认。",
			EvidenceIDs: []string{"metric-1", "memory-1"},
		}},
		Recommendations: []agenteino.Recommendation{{
			Text:        "检查 payment 服务实时延迟和错误率。",
			EvidenceIDs: []string{"metric-1", "memory-1"},
		}},
		Limitations: []agenteino.Limitation{{
			Code:    "TRACES_NO_DATA",
			Message: "Trace evidence is missing.",
			Tool:    "query_traces",
		}, {
			Code:    "AGENT_REPEATED_TOOL_CALL",
			Message: "Repeated tool call was blocked.",
			Tool:    "query_metrics",
		}},
		Evidence: input.Evidence,
		ToolRuns: []agenteino.ToolRun{{
			Tool:            "query_traces",
			Success:         true,
			EvidenceCount:   0,
			ExecutionStatus: "success",
			DataStatus:      "fallback",
			FallbackUsed:    true,
		}},
		Metadata: map[string]any{"synthesis_llm_success": true},
	}

	diagnosis := buildFinalDiagnosis(output, input)
	if diagnosis.RootCause.Conclusion == output.Conclusions[0].Text {
		t.Fatalf("root cause reused symptom finding: %#v", diagnosis.RootCause)
	}
	if diagnosis.RootCause.Status == "confirmed" || diagnosis.RootCause.Confidence == "high" {
		t.Fatalf("root cause overconfident without logs/traces: %#v", diagnosis.RootCause)
	}
	if len(diagnosis.Findings) == 0 || diagnosis.Findings[0].Kind != "fact" {
		t.Fatalf("metric symptom finding kind = %#v", diagnosis.Findings)
	}
	historical := false
	for _, finding := range diagnosis.Findings {
		if finding.Kind == "historical_match" {
			historical = true
		}
		if finding.Title == "结论 1" || finding.Title == "推断 1" {
			t.Fatalf("non-informative finding title survived: %#v", finding)
		}
	}
	if !historical {
		t.Fatalf("historical memory was not classified as historical_match: %#v", diagnosis.Findings)
	}
	hasTraceLimitation := false
	for _, limitation := range diagnosis.Limitations {
		if limitation.Code == "TRACES_NO_DATA" {
			hasTraceLimitation = true
		}
	}
	if !hasTraceLimitation {
		t.Fatalf("diagnostic limitations = %#v", diagnosis.Limitations)
	}
	if len(diagnosis.ExecutionWarnings) < 2 {
		t.Fatalf("execution warnings missing agent/tool warnings: %#v", diagnosis.ExecutionWarnings)
	}
	if diagnosis.Recommendations[0].Reason == diagnosis.Recommendations[0].Risk {
		t.Fatalf("recommendation text not specific: %#v", diagnosis.Recommendations[0])
	}
}

func TestExecutionWarningsRespectSingleDataStatus(t *testing.T) {
	tests := []struct {
		name       string
		run        agenteino.ToolRun
		wantCode   string
		wantLength int
	}{
		{
			name:       "empty creates no data limitation elsewhere but no partial warning",
			run:        agenteino.ToolRun{Tool: "query_traces", DataStatus: "empty", WarningCount: 1},
			wantLength: 0,
		},
		{
			name:       "partial creates partial warning",
			run:        agenteino.ToolRun{Tool: "query_traces", DataStatus: "partial", WarningCount: 1},
			wantCode:   "TOOL_DATA_PARTIAL",
			wantLength: 1,
		},
		{
			name:       "fallback creates fallback warning",
			run:        agenteino.ToolRun{Tool: "query_traces", DataStatus: "fallback", WarningCount: 1},
			wantCode:   "TOOL_FALLBACK_USED",
			wantLength: 1,
		},
		{
			name:       "available creates no data quality warning",
			run:        agenteino.ToolRun{Tool: "query_traces", DataStatus: "available", WarningCount: 0},
			wantLength: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := executionWarningsFromToolRuns([]agenteino.ToolRun{test.run}, "zh")
			if len(got) != test.wantLength {
				t.Fatalf("warning count = %d, want %d: %#v", len(got), test.wantLength, got)
			}
			if test.wantCode != "" && got[0].Code != test.wantCode {
				t.Fatalf("warning code = %s, want %s", got[0].Code, test.wantCode)
			}
		})
	}
}

func TestHistoricalMemoryEvidenceIsContextual(t *testing.T) {
	memory := common.EvidenceItem{
		ID:         "memory-1",
		SourceType: "memory",
		SourceName: "long-term-memory",
		Content:    "Prior checkout incident matched payment latency.",
		Metadata: map[string]any{
			"evidence_origin": "historical_memory",
		},
	}

	if got := evidenceWeight(memory); got != "contextual" {
		t.Fatalf("evidenceWeight(memory) = %q, want contextual", got)
	}
	refs := evidenceReferencesFromItems([]common.EvidenceItem{memory}, "en")
	if len(refs) != 1 || refs[0].EvidenceWeight != "contextual" ||
		refs[0].EvidenceOrigin != "historical_memory" ||
		refs[0].CanConfirmCurrentFact {
		t.Fatalf("memory evidence refs = %#v", refs)
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
