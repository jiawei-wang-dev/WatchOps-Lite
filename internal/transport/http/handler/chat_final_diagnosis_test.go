package handler

import (
	"strings"
	"testing"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/multiagent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func TestMapChatResponseAddsFinalDiagnosisForSingleAgent(t *testing.T) {
	response := mapChatResponse(applicationchat.Result{
		RequestID: "req-1",
		SessionID: "session-1",
		Agent: agenteino.AgentOutput{
			Conclusions: []agenteino.Conclusion{{
				Text:        "Checkout error rate increased.",
				EvidenceIDs: []string{"metrics-1"},
			}},
			Inferences: []agenteino.Inference{{
				Text:        "Payment dependency timeout is a possible contributor.",
				EvidenceIDs: []string{"metrics-1"},
			}},
			Recommendations: []agenteino.Recommendation{{
				Text:        "Validate payment dependency latency before remediation.",
				EvidenceIDs: []string{"metrics-1"},
			}},
			Limitations: []agenteino.Limitation{{
				Code:    "NEEDS_TRACE",
				Message: "Trace evidence is not available yet.",
				Tool:    "query_traces",
			}},
			Evidence: []common.EvidenceItem{{
				ID:         "metrics-1",
				SourceType: "metrics",
				SourceName: "prometheus",
				Content:    "watchops_checkout_error_rate=0.062",
				ResourceID: "checkout",
				Metadata: map[string]any{
					"service": "checkout",
				},
			}},
			Metadata: map[string]any{
				"requested_language": "en-US",
			},
		},
	})

	if response.Metadata["execution_mode"] != "single_agent" {
		t.Fatalf("execution_mode = %v, want single_agent", response.Metadata["execution_mode"])
	}
	if response.Metadata["final_diagnosis_schema_version"] != "watchops.final_diagnosis.v1" {
		t.Fatalf("schema version = %v", response.Metadata["final_diagnosis_schema_version"])
	}
	diagnosis, ok := response.Metadata["final_diagnosis"].(multiagent.FinalDiagnosis)
	if !ok {
		t.Fatalf("final_diagnosis type = %T, want multiagent.FinalDiagnosis", response.Metadata["final_diagnosis"])
	}
	if diagnosis.ExecutionMode != "single_agent" {
		t.Fatalf("diagnosis execution mode = %q", diagnosis.ExecutionMode)
	}
	if diagnosis.Incident.Service != "checkout" {
		t.Fatalf("service = %q, want checkout", diagnosis.Incident.Service)
	}
	if len(diagnosis.Findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(diagnosis.Findings))
	}
	if diagnosis.Findings[0].Kind != "fact" || diagnosis.Findings[1].Kind != "hypothesis" {
		t.Fatalf("finding kinds = %#v", diagnosis.Findings)
	}
	if diagnosis.RootCause.Status == "confirmed" {
		t.Fatalf("single-agent adapter must not mark root cause confirmed")
	}
	if len(diagnosis.EvidenceRefs) != 1 || diagnosis.EvidenceRefs[0].ID != "metrics-1" {
		t.Fatalf("evidence refs = %#v", diagnosis.EvidenceRefs)
	}
	if len(diagnosis.Limitations) != 1 || diagnosis.Limitations[0].Source != "query_traces" {
		t.Fatalf("limitations = %#v", diagnosis.Limitations)
	}
}

func TestSingleAgentFinalDiagnosisUsesResponseLanguage(t *testing.T) {
	response := mapChatResponse(applicationchat.Result{
		RequestID: "req-zh",
		SessionID: "session-zh",
		Agent: agenteino.AgentOutput{
			Inferences: []agenteino.Inference{{
				Text:        "payment 依赖延迟可能导致 checkout 错误率升高。",
				EvidenceIDs: []string{"metrics-1"},
			}},
			Recommendations: []agenteino.Recommendation{{
				Text:        "检查 payment 服务实时延迟。",
				EvidenceIDs: []string{"metrics-1"},
			}},
			Evidence: []common.EvidenceItem{{
				ID:         "metrics-1",
				SourceType: "metrics",
				SourceName: "prometheus",
				Content:    "watchops_checkout_error_rate=0.062",
				ResourceID: "checkout",
				Metadata: map[string]any{
					"service": "checkout",
				},
			}},
			Metadata: map[string]any{
				"response_language": "zh",
			},
		},
	})

	diagnosis, ok := response.Metadata["final_diagnosis"].(multiagent.FinalDiagnosis)
	if !ok {
		t.Fatalf("final_diagnosis type = %T", response.Metadata["final_diagnosis"])
	}
	if diagnosis.Language != "zh-CN" {
		t.Fatalf("language = %q, want zh-CN", diagnosis.Language)
	}
	if strings.Contains(diagnosis.RootCause.Conclusion, "current dependency metrics are missing") {
		t.Fatalf("Chinese root cause contains English fallback: %q", diagnosis.RootCause.Conclusion)
	}
	if !strings.Contains(diagnosis.RootCause.Conclusion, "依赖服务实时指标") {
		t.Fatalf("root cause conclusion = %q, want Chinese dependency-metrics boundary", diagnosis.RootCause.Conclusion)
	}
}
