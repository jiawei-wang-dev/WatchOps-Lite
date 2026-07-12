package multiagent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
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
			"suspected_services":["checkout"],
			"incident_type":"high_error_rate",
			"evidence_plan":["metrics","logs","traces","knowledge"],
			"hypotheses":[{"statement":"Checkout may have elevated errors and needs evidence review.","requires_verification":true}],
			"uncertainties":["Root cause is not confirmed."],
			"language":"en"
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
	options := einomodel.GetCommonOptions(nil, llm.model.(*analysisModelStub).options...)
	if options.MaxTokens == nil || *options.MaxTokens != 512 {
		t.Fatalf("MaxTokens = %#v, want 512", options.MaxTokens)
	}
	if options.Temperature == nil || *options.Temperature != 0 {
		t.Fatalf("Temperature = %#v, want 0", options.Temperature)
	}
}

func TestLLMTriagePromptIncludesSharedConstraints(t *testing.T) {
	model := &analysisModelStub{response: schema.AssistantMessage(`{
		"suspected_services":["payment"],
		"incident_type":"timeout",
		"evidence_plan":["metrics","logs","traces"],
		"hypotheses":[{"statement":"payment 可能存在待验证的 timeout 信号。","requires_verification":true}],
		"uncertainties":["根因尚未确认。"],
		"language":"zh-CN"
	}`, nil)}
	llm, err := NewRoleLLM(model, "test-model", time.Second)
	if err != nil {
		t.Fatalf("NewRoleLLM() error = %v", err)
	}
	plan, err := NewLLMTriageAgent("checkout", llm).Plan(context.Background(), Input{
		Message: "请排查 payment timeout",
		Metadata: map[string]any{
			"requested_language": "zh-CN",
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Service != "payment" || plan.Metadata["requested_language"] != "zh-CN" {
		t.Fatalf("plan = %#v", plan)
	}
	systemPrompt := model.messages[0].Content
	if !strings.Contains(systemPrompt, `"payment"`) ||
		!strings.Contains(systemPrompt, `exact literal "zh-CN"`) ||
		!strings.Contains(systemPrompt, "Do not invent services") {
		t.Fatalf("system prompt missing constraints:\n%s", systemPrompt)
	}
}

func TestValidateTriageRejectsUnsupportedServiceFromConstraints(t *testing.T) {
	err := validateTriageLLMOutput(triageLLMOutput{
		SuspectedServices: []string{"user"},
		IncidentType:      IncidentHighErrorRate,
		EvidencePlan:      []string{"metrics"},
		Hypotheses: []triageHypothesis{{
			Statement:            "Checkout may have an unverified signal.",
			RequiresVerification: true,
		}},
		Uncertainties: []string{"Root cause is not confirmed."},
		Language:      "en-US",
	}, TriageConstraints{
		AllowedServices:      []string{"checkout"},
		AllowedIncidentTypes: supportedIncidentTypes(),
		RequestedLanguage:    "en-US",
	})
	var violation RoleContractViolation
	if !errors.As(err, &violation) || violation.Field != "suspected_services" {
		t.Fatalf("err = %v, want suspected_services contract violation", err)
	}
}

func TestValidateTriageAllowsEmptyServiceWhenUnknown(t *testing.T) {
	err := validateTriageLLMOutput(triageLLMOutput{
		SuspectedServices: []string{},
		IncidentType:      IncidentUnknown,
		EvidencePlan:      []string{"metrics"},
		Hypotheses: []triageHypothesis{{
			Statement:            "The affected service is unknown and requires evidence review.",
			RequiresVerification: true,
		}},
		Uncertainties: []string{"Service scope is not confirmed."},
		Language:      "en-US",
	}, TriageConstraints{
		AllowedServices:      []string{},
		AllowedIncidentTypes: supportedIncidentTypes(),
		RequestedLanguage:    "en-US",
	})
	if err != nil {
		t.Fatalf("validateTriageLLMOutput() error = %v", err)
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
			"suspected_services":["checkout"],
			"incident_type":"high_error_rate",
			"evidence_plan":["database_magic"],
			"hypotheses":[{"statement":"Checkout errors require verification.","requires_verification":true}],
			"uncertainties":["Root cause is not confirmed."],
			"language":"en"
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
			"suspected_services":["checkout"],
			"incident_type":"high_error_rate",
			"evidence_plan":["metrics"],
			"hypotheses":[{"statement":"The root cause is payment timeout.","requires_verification":true}],
			"uncertainties":[],
			"language":"en"
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
		plan.Metadata["triage_fallback_reason"] != "invalid_output" ||
		plan.Metadata["triage_llm_error_code"] != "role_contract_violation" {
		t.Fatalf("metadata = %#v", plan.Metadata)
	}
}

func TestLLMTriageAgentAllowsUnconfirmedRootCauseBoundary(t *testing.T) {
	llm, err := NewRoleLLM(
		&analysisModelStub{response: schema.AssistantMessage(`{
			"suspected_services":["checkout"],
			"incident_type":"high_error_rate",
			"evidence_plan":["metrics","logs"],
			"hypotheses":[{"statement":"Checkout may have elevated errors and requires evidence review.","requires_verification":true}],
			"uncertainties":["Root cause is not confirmed."],
			"language":"en"
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
	if plan.Metadata["triage_llm_used"] != true ||
		plan.Metadata["triage_fallback_used"] != false {
		t.Fatalf("metadata = %#v", plan.Metadata)
	}
}

func TestLLMTriageAgentRepairsRoleContractViolation(t *testing.T) {
	llm, err := NewRoleLLM(
		&analysisModelStub{responses: []*schema.Message{
			schema.AssistantMessage(`{
				"suspected_services":["checkout"],
				"incident_type":"high_error_rate",
				"evidence_plan":["metrics"],
				"hypotheses":[{"statement":"The root cause is payment timeout.","requires_verification":true}],
				"uncertainties":[],
				"language":"en"
			}`, nil),
			schema.AssistantMessage(`{
				"suspected_services":["checkout"],
				"incident_type":"high_error_rate",
				"evidence_plan":["metrics","logs","traces"],
				"hypotheses":[{"statement":"Checkout error rate may be elevated and requires verification.","requires_verification":true}],
				"uncertainties":["Root cause is not confirmed."],
				"language":"en"
			}`, nil),
		}},
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
	if plan.Metadata["triage_llm_used"] != true ||
		plan.Metadata["triage_fallback_used"] != false ||
		plan.Metadata["triage_llm_retry_count"] != 1 ||
		plan.Metadata["triage_recovery_success"] != true ||
		plan.Metadata["triage_analysis_mode"] != "llm_repaired" {
		t.Fatalf("metadata = %#v", plan.Metadata)
	}
	if _, ok := plan.Metadata["triage_llm_primary_elapsed_ms"]; !ok {
		t.Fatalf("missing triage_llm_primary_elapsed_ms metadata: %#v", plan.Metadata)
	}
	if _, ok := plan.Metadata["triage_llm_repair_elapsed_ms"]; !ok {
		t.Fatalf("missing triage_llm_repair_elapsed_ms metadata: %#v", plan.Metadata)
	}
	if timeout, ok := plan.Metadata["triage_repair_llm_timeout_ms"].(int64); !ok || timeout <= 0 {
		t.Fatalf("triage_repair_llm_timeout_ms = %#v, want > 0", plan.Metadata["triage_repair_llm_timeout_ms"])
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
