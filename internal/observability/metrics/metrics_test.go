package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCollectorExposesRecordedMetrics(t *testing.T) {
	collector := New()
	SetDefault(collector)
	t.Cleanup(func() { SetDefault(nil) })

	ObserveHTTPRequest("GET", "/healthz", "200", 10*time.Millisecond)
	ObserveChat(true, 20*time.Millisecond)
	ObserveTool("query_logs", "", 30*time.Millisecond)
	ObserveTool("query_logs", "TOOL_TIMEOUT", 40*time.Millisecond)
	ObserveRAGSearch("hybrid", 50*time.Millisecond)
	IncSessionMemoryUnavailable()
	IncAgentFallback("llm_unavailable")
	IncSummaryFallback("parse")
	IncEvalRun("completed")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/metrics", nil)
	collector.Handler().ServeHTTP(recorder, request)

	if recorder.Code != 200 {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	for _, expected := range []string{
		`watchops_http_requests_total{method="GET",route="/healthz",status="200"} 1`,
		`watchops_chat_requests_total{status="success"} 1`,
		`watchops_tool_calls_total{status="success",tool="query_logs"} 1`,
		`watchops_tool_errors_total{error_code="TOOL_TIMEOUT",tool="query_logs"} 1`,
		`watchops_session_memory_unavailable_total 1`,
		`watchops_agent_fallback_total{reason="llm_unavailable"} 1`,
		`watchops_summary_fallback_total{reason="parse"} 1`,
		`watchops_eval_runs_total{status="completed"} 1`,
	} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Errorf("metrics output does not contain %q", expected)
		}
	}
}

func TestDisabledDefaultIsNoOp(t *testing.T) {
	SetDefault(nil)
	ObserveHTTPRequest("GET", "/healthz", "200", time.Millisecond)
	ObserveChat(false, time.Millisecond)
	ObserveTool("query_logs", "TOOL_INTERNAL", time.Millisecond)
	ObserveRAGSearch("bm25", time.Millisecond)
	IncSessionMemoryUnavailable()
	IncAgentFallback("test")
	IncSummaryFallback("test")
	IncEvalRun("failed")
}
