package multiagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type analysisModelStub struct {
	response *schema.Message
	err      error
	messages []*schema.Message
	generate func(context.Context) (*schema.Message, error)
}

func (s *analysisModelStub) Generate(
	ctx context.Context,
	messages []*schema.Message,
	_ ...model.Option,
) (*schema.Message, error) {
	s.messages = messages
	if s.generate != nil {
		return s.generate(ctx)
	}
	return s.response, s.err
}

func TestRoleLLMAnalyzesEvidenceWithAllowlistedIDs(t *testing.T) {
	model := &analysisModelStub{response: schema.AssistantMessage(`{
		"observation_summary":"Checkout error rate and latency increased.",
		"supported_signals":["elevated errors","higher latency"],
		"suspected_failure_pattern":"dependency timeout",
		"missing_evidence":["matching trace"],
		"evidence_ids":["metric-1"]
	}`, nil)}
	llm, err := NewRoleLLM(model, "test-model", time.Second)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	evidence := synthesisFixture("en").Evidence[:1]
	analysis, _, err := llm.analyzeEvidence(
		context.Background(),
		TriagePlan{Service: "checkout", Language: "en"},
		evidence,
		nil,
	)
	if err != nil {
		t.Fatalf("analyzeEvidence() error = %v", err)
	}
	if analysis.ObservationSummary == "" ||
		len(analysis.EvidenceIDs) != 1 ||
		analysis.EvidenceIDs[0] != "metric-1" {
		t.Fatalf("analysis = %#v", analysis)
	}
	if len(model.messages) != 2 ||
		!strings.Contains(model.messages[1].Content, "metric-1") {
		t.Fatalf("prompt messages = %#v", model.messages)
	}
}

func TestRoleLLMRejectsInventedEvidenceID(t *testing.T) {
	model := &analysisModelStub{response: schema.AssistantMessage(`{
		"observation_summary":"Unsupported",
		"supported_signals":[],
		"suspected_failure_pattern":"",
		"missing_evidence":[],
		"evidence_ids":["invented"]
	}`, nil)}
	llm, err := NewRoleLLM(model, "test-model", time.Second)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	if _, _, err := llm.analyzeEvidence(
		context.Background(),
		TriagePlan{Language: "en"},
		synthesisFixture("en").Evidence[:1],
		nil,
	); err == nil {
		t.Fatal("analyzeEvidence() error = nil, want unknown evidence rejection")
	}
}

func TestLLMSynthesizerProducesExistingAnswerSchema(t *testing.T) {
	model := &analysisModelStub{response: schema.AssistantMessage(`{
		"conclusions":[{"text":"Checkout has an observed error signal.","evidence_ids":["metric-1"]}],
		"inferences":[{"text":"The runbook scenario may apply.","evidence_ids":["metric-1","runbook-1"]}],
		"recommendations":[{"text":"Validate the cited mitigation.","evidence_ids":["runbook-1"]}],
		"limitations":[{"code":"ROOT_CAUSE_UNCONFIRMED","message":"Confirm with a matching trace."}]
	}`, nil)}
	llm, err := NewRoleLLM(model, "test-model", time.Second)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	output, err := NewSynthesisAgent(NewLLMSynthesizer(llm)).Synthesize(
		context.Background(),
		synthesisFixture("en"),
	)
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if output.Metadata["synthesis_llm_used"] != true ||
		output.Metadata["synthesis_fallback_used"] != false {
		t.Fatalf("metadata = %#v", output.Metadata)
	}
	if len(output.Conclusions) != 1 || len(output.Evidence) != 2 {
		t.Fatalf("output = %#v", output)
	}
}

func TestLLMSynthesizerInvalidJSONFallsBackDeterministically(t *testing.T) {
	model := &analysisModelStub{
		response: schema.AssistantMessage("not-json", nil),
	}
	llm, err := NewRoleLLM(model, "test-model", time.Second)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	output, err := NewSynthesisAgent(NewLLMSynthesizer(llm)).Synthesize(
		context.Background(),
		synthesisFixture("en"),
	)
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if output.Metadata["synthesis_llm_used"] != false ||
		output.Metadata["synthesis_fallback_used"] != true ||
		len(output.Conclusions) == 0 {
		t.Fatalf("output = %#v", output)
	}
}

func TestEvidenceAgentLLMFailureKeepsDeterministicSummary(t *testing.T) {
	tools, err := agenteino.BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}
	llm, err := NewRoleLLM(
		&analysisModelStub{response: schema.AssistantMessage("not-json", nil)},
		"test-model",
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	agent, err := NewEvidenceAgent(context.Background(), tools)
	if err != nil {
		t.Fatalf("NewEvidenceAgent() error = %v", err)
	}
	finding, err := agent.WithLLM(llm).Analyze(context.Background(), TriagePlan{
		Service:      "checkout",
		IncidentType: IncidentHighErrorRate,
		EvidencePlan: []string{"metrics"},
		Language:     "en",
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if !strings.Contains(finding.Summary, "Observability summary") ||
		finding.Metadata["evidence_llm_used"] != false ||
		finding.Metadata["evidence_fallback_used"] != true {
		t.Fatalf("finding = %#v", finding)
	}
}

func TestRoleLLMHonorsTimeout(t *testing.T) {
	model := &analysisModelStub{generate: func(ctx context.Context) (*schema.Message, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	llm, err := NewRoleLLM(model, "test-model", time.Millisecond)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	if _, _, err := llm.analyzeEvidence(
		context.Background(),
		TriagePlan{Language: "en"},
		synthesisFixture("en").Evidence[:1],
		nil,
	); err == nil {
		t.Fatal("analyzeEvidence() error = nil, want timeout")
	}
}
