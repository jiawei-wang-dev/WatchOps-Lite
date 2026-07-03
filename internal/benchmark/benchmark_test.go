package benchmark

import (
	"os"
	"strings"
	"testing"
)

func TestLoadCases(t *testing.T) {
	cases, err := LoadCases(strings.NewReader(`[
		{
			"id": "checkout-errors",
			"session_id": "benchmark-checkout",
			"message": "Why is checkout failing?",
			"expected_behavior": "Inspect evidence.",
			"notes": "Local demo case."
		}
	]`))
	if err != nil {
		t.Fatalf("LoadCases() error = %v", err)
	}
	if len(cases) != 1 || cases[0].ID != "checkout-errors" {
		t.Fatalf("cases = %#v", cases)
	}
}

func TestRepositoryBenchmarkCasesLoad(t *testing.T) {
	file, err := os.Open("../../testdata/agent_benchmark_cases.json")
	if err != nil {
		t.Fatalf("open repository cases: %v", err)
	}
	defer file.Close()
	cases, err := LoadCases(file)
	if err != nil {
		t.Fatalf("LoadCases() error = %v", err)
	}
	if len(cases) != 6 {
		t.Fatalf("case count = %d, want 6", len(cases))
	}
}

func TestChineseRepositoryBenchmarkCasesLoad(t *testing.T) {
	file, err := os.Open("../../testdata/agent_benchmark_cases_zh.json")
	if err != nil {
		t.Fatalf("open Chinese repository cases: %v", err)
	}
	defer file.Close()
	cases, err := LoadCases(file)
	if err != nil {
		t.Fatalf("LoadCases() error = %v", err)
	}
	if len(cases) != 3 {
		t.Fatalf("Chinese case count = %d, want 3", len(cases))
	}
	for _, current := range cases {
		if !strings.ContainsAny(current.Message, "服务错误率支付超时告警证据") {
			t.Fatalf("case %s does not contain Chinese text", current.ID)
		}
	}
}

func TestPercentileNearestRank(t *testing.T) {
	values := make([]float64, 100)
	for index := range values {
		values[index] = float64(index + 1)
	}
	if got := PercentileNearestRank(values, 0.95); got != 95 {
		t.Fatalf("p95 = %v, want 95", got)
	}
}

func TestCalculateSummary(t *testing.T) {
	summary := CalculateSummary([]CaseResult{
		{
			Success:          true,
			LatencyMS:        100,
			ToolRuns:         2,
			EvidenceCount:    3,
			RequestIDPresent: true,
			TraceIDPresent:   true,
		},
		{
			Success:           false,
			LatencyMS:         300,
			ToolRuns:          4,
			FallbackDetected:  true,
			LimitationCount:   2,
			RequestIDPresent:  true,
			FailureController: true,
		},
	})
	if summary.TotalRequests != 2 ||
		summary.SuccessfulRequests != 1 ||
		summary.FailedRequests != 1 ||
		summary.SuccessRate != 0.5 ||
		summary.AverageLatencyMS != 200 ||
		summary.MinLatencyMS != 100 ||
		summary.MaxLatencyMS != 300 ||
		summary.P95LatencyMS != 300 ||
		summary.AverageToolRuns != 3 ||
		summary.AverageEvidenceCount != 1.5 ||
		summary.FallbackCount != 1 ||
		summary.LimitationCount != 2 ||
		summary.EmptyEvidenceCount != 1 ||
		summary.RequestIDPresenceRate != 1 ||
		summary.TraceIDPresenceRate != 0.5 ||
		summary.FailureControllerHitCount != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestCalculateSummaryEmpty(t *testing.T) {
	summary := CalculateSummary(nil)
	if summary.TotalRequests != 0 || summary.P95LatencyMS != 0 || summary.SuccessRate != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}
