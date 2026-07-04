package multiagent

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestDeterministicTriagePlansChineseCheckoutInvestigation(t *testing.T) {
	agent := NewDeterministicTriageAgent("checkout")
	plan, err := agent.Plan(context.Background(), Input{
		Message: "checkout 服务错误率为什么升高？请结合指标、日志、告警和 runbook 给出有证据的诊断。",
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Service != "checkout" ||
		plan.IncidentType != IncidentHighErrorRate ||
		plan.Language != "zh" {
		t.Fatalf("plan = %#v", plan)
	}
	wantSources := []string{
		"metrics",
		"logs",
		"alerts",
		"traces",
		"topology",
		"knowledge",
	}
	if !reflect.DeepEqual(plan.EvidencePlan, wantSources) {
		t.Fatalf("EvidencePlan = %#v, want %#v", plan.EvidencePlan, wantSources)
	}
	if !strings.Contains(plan.Summary, "service=checkout") ||
		!strings.Contains(plan.Summary, "evidence_plan=") {
		t.Fatalf("Summary = %q", plan.Summary)
	}
	if len(plan.Limitations) != 0 {
		t.Fatalf("Limitations = %#v, want none", plan.Limitations)
	}
}

func TestDeterministicTriageKeepsEnglishTechnicalIdentifiers(t *testing.T) {
	agent := NewDeterministicTriageAgent("checkout")
	plan, err := agent.Plan(context.Background(), Input{
		Message: "Investigate payment timeout latency with traces and topology.",
		TimeContext: common.TimeRange{
			From: "2026-07-03T00:00:00Z",
			To:   "2026-07-03T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Service != "payment" ||
		plan.IncidentType != IncidentPaymentTimeout ||
		plan.Language != "en" {
		t.Fatalf("plan = %#v", plan)
	}
	if !strings.Contains(plan.Summary, "service=payment") {
		t.Fatalf("Summary = %q", plan.Summary)
	}
}

func TestDeterministicTriageUsesBoundedFallbackForUnknownService(t *testing.T) {
	agent := NewDeterministicTriageAgent("")
	plan, err := agent.Plan(context.Background(), Input{
		Message: "为什么最近请求很慢？",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Service != "checkout" || plan.IncidentType != IncidentLatency {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.Limitations) != 1 ||
		plan.Limitations[0].Code != "TRIAGE_SERVICE_UNCERTAIN" {
		t.Fatalf("Limitations = %#v", plan.Limitations)
	}
	if len(plan.EvidencePlan) > maxEvidencePlanSize {
		t.Fatalf(
			"EvidencePlan length = %d, want <= %d",
			len(plan.EvidencePlan),
			maxEvidencePlanSize,
		)
	}
}

func TestLLMTriageAgentUsesValidLLMPlan(t *testing.T) {
	llm, err := NewRoleLLM(
		&analysisModelStub{response: schema.AssistantMessage(`{
			"service":"checkout",
			"incident_type":"high_error_rate",
			"severity_hint":"warning",
			"evidence_plan":["metrics","logs","traces","knowledge"],
			"language":"en",
			"time_window_reason":"Use the supplied incident time range.",
			"triage_summary":"Investigate checkout high error rate using metrics, logs, traces, and knowledge.",
			"constraints":["Do not claim root cause before evidence is reviewed."]
		}`, nil)},
		"test-model",
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	plan, err := NewLLMTriageAgent("checkout", llm).Plan(context.Background(), Input{
		Message: "Investigate checkout error rate.",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Service != "checkout" ||
		plan.IncidentType != IncidentHighErrorRate ||
		plan.Metadata["triage_llm_used"] != true ||
		plan.Metadata["triage_fallback_used"] != false ||
		plan.Metadata["triage_model"] != "test-model" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestLLMTriageAgentInvalidJSONFallsBack(t *testing.T) {
	llm, err := NewRoleLLM(
		&analysisModelStub{response: schema.AssistantMessage("not-json", nil)},
		"test-model",
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	plan, err := NewLLMTriageAgent("checkout", llm).Plan(context.Background(), Input{
		Message: "Investigate checkout error rate.",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Metadata["triage_llm_used"] != false ||
		plan.Metadata["triage_llm_attempted"] != true ||
		plan.Metadata["triage_fallback_used"] != true ||
		plan.Metadata["triage_fallback_reason"] != "invalid_json" {
		t.Fatalf("metadata = %#v", plan.Metadata)
	}
}

func TestLLMTriageAgentInvalidEvidencePlanFallsBack(t *testing.T) {
	llm, err := NewRoleLLM(
		&analysisModelStub{response: schema.AssistantMessage(`{
			"service":"checkout",
			"incident_type":"high_error_rate",
			"severity_hint":"warning",
			"evidence_plan":["database_magic"],
			"language":"en",
			"time_window_reason":"Use the supplied incident time range.",
			"triage_summary":"Investigate checkout high error rate.",
			"constraints":[]
		}`, nil)},
		"test-model",
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	plan, err := NewLLMTriageAgent("checkout", llm).Plan(context.Background(), Input{
		Message: "Investigate checkout error rate.",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Metadata["triage_fallback_used"] != true ||
		plan.Metadata["triage_fallback_reason"] != "invalid_output" {
		t.Fatalf("metadata = %#v", plan.Metadata)
	}
}

func TestLLMTriageAgentRejectsFinalDiagnosisClaim(t *testing.T) {
	llm, err := NewRoleLLM(
		&analysisModelStub{response: schema.AssistantMessage(`{
			"service":"checkout",
			"incident_type":"high_error_rate",
			"severity_hint":"critical",
			"evidence_plan":["metrics","logs"],
			"language":"en",
			"time_window_reason":"Use the supplied incident time range.",
			"triage_summary":"The root cause is payment timeout.",
			"constraints":[]
		}`, nil)},
		"test-model",
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	plan, err := NewLLMTriageAgent("checkout", llm).Plan(context.Background(), Input{
		Message: "Investigate checkout error rate.",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Metadata["triage_fallback_used"] != true ||
		plan.Metadata["triage_fallback_reason"] != "invalid_output" {
		t.Fatalf("metadata = %#v", plan.Metadata)
	}
}

func TestLLMTriageAgentWithoutLLMKeepsDeterministicMetadata(t *testing.T) {
	plan, err := NewLLMTriageAgent("checkout", nil).Plan(context.Background(), Input{
		Message: "Investigate checkout error rate.",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Metadata["triage_llm_attempted"] != false ||
		plan.Metadata["triage_llm_used"] != false ||
		plan.Metadata["triage_fallback_used"] != true {
		t.Fatalf("metadata = %#v", plan.Metadata)
	}
}
