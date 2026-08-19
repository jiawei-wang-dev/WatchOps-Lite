package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type evalCase struct {
	CaseID   string `json:"case_id"`
	Category string `json:"category"`
	Input    string `json:"input"`
}

type caseResult struct {
	CaseID           string   `json:"case_id"`
	Category         string   `json:"category"`
	Input            string   `json:"input"`
	ExpectedIntent   string   `json:"expected_intent"`
	ExpectedTools    []string `json:"expected_tools"`
	ExpectedDecision string   `json:"expected_decision"`
	ActualIntent     string   `json:"actual_intent"`
	ActualTools      []string `json:"actual_tools"`
	ActualEvidence   []string `json:"actual_evidence"`
	FinalStatus      string   `json:"final_status"`
	FailureReason    string   `json:"failure_reason,omitempty"`
	LatencyMS        float64  `json:"latency_ms"`
}

type report struct {
	GeneratedAt    time.Time    `json:"generated_at"`
	Enabled        bool         `json:"enabled"`
	ExternalLLM    bool         `json:"external_llm"`
	RequestedCases int          `json:"requested_cases"`
	ExecutedCases  int          `json:"executed_cases"`
	SkippedReason  string       `json:"skipped_reason,omitempty"`
	Cases          []caseResult `json:"cases"`
}

type chatResponse struct {
	Answer struct {
		Evidence []struct {
			ID string `json:"id"`
		} `json:"evidence"`
		Limitations []struct {
			Code string `json:"code"`
		} `json:"limitations"`
	} `json:"answer"`
	ToolRuns []struct {
		Tool string `json:"tool"`
	} `json:"tool_runs"`
	Metadata map[string]any `json:"metadata"`
}

func main() {
	cases := loadCases("testdata/agent_e2e_cases.json")
	maxCases := envInt("WATCHOPS_EVAL_MAX_LLM_CASES", 20)
	if maxCases > 30 {
		maxCases = 30
	}
	if maxCases < 1 {
		maxCases = 1
	}
	if maxCases > len(cases) {
		maxCases = len(cases)
	}
	enabled := strings.EqualFold(strings.TrimSpace(os.Getenv("WATCHOPS_EVAL_LLM_ENABLED")), "true")
	fmt.Printf("Agent LLM evaluation planned case count: %d (hard cap 30)\n", maxCases)
	result := report{
		GeneratedAt: time.Now().UTC(), Enabled: enabled, ExternalLLM: enabled,
		RequestedCases: maxCases, Cases: []caseResult{},
	}
	if !enabled {
		result.ExternalLLM = false
		result.SkippedReason = "external LLM evaluation is opt-in; WATCHOPS_EVAL_LLM_ENABLED is not true"
		writeReport(result)
		fmt.Println("skipped because external LLM evaluation is opt-in")
		return
	}
	if strings.TrimSpace(os.Getenv("WATCHOPS_LLM_API_KEY")) == "" {
		result.ExternalLLM = false
		result.SkippedReason = "WATCHOPS_LLM_API_KEY is not configured"
		writeReport(result)
		fmt.Println("skipped because WATCHOPS_LLM_API_KEY is not configured")
		return
	}

	baseURL := strings.TrimRight(envString("WATCHOPS_API_BASE_URL", "http://localhost:8080"), "/")
	client := &http.Client{Timeout: 45 * time.Second}
	for _, item := range cases[:maxCases] {
		result.Cases = append(result.Cases, runCase(client, baseURL, item))
	}
	result.ExecutedCases = len(result.Cases)
	writeReport(result)
	for _, item := range result.Cases {
		if item.FinalStatus == "failed" || item.FinalStatus == "timeout" {
			os.Exit(1)
		}
	}
}

func loadCases(path string) []evalCase {
	file, err := os.Open(path)
	if err != nil {
		fatalf("open cases: %v", err)
	}
	defer file.Close()
	var cases []evalCase
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		fatalf("decode cases: %v", err)
	}
	if len(cases) == 0 {
		fatalf("agent cases are empty")
	}
	return cases
}

func runCase(client *http.Client, baseURL string, item evalCase) caseResult {
	expectedIntent, expectedTools, expectedDecision := expectations(item.Category)
	result := caseResult{
		CaseID: item.CaseID, Category: item.Category, Input: item.Input,
		ExpectedIntent: expectedIntent, ExpectedTools: expectedTools,
		ExpectedDecision: expectedDecision,
	}
	payload, _ := json.Marshal(map[string]any{
		"session_id": "llm-eval-" + item.CaseID,
		"message":    item.Input,
		"time_context": map[string]string{
			"from": time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339),
			"to":   time.Now().UTC().Format(time.RFC3339),
		},
	})
	started := time.Now()
	response, err := client.Post(baseURL+"/api/v1/chat", "application/json", bytes.NewReader(payload))
	result.LatencyMS = float64(time.Since(started).Nanoseconds()) / 1e6
	if err != nil {
		result.FinalStatus = "timeout"
		result.FailureReason = err.Error()
		return result
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.FinalStatus = "failed"
		result.FailureReason = fmt.Sprintf("HTTP %d", response.StatusCode)
		return result
	}
	var decoded chatResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		result.FinalStatus = "failed"
		result.FailureReason = "invalid response JSON"
		return result
	}
	for _, run := range decoded.ToolRuns {
		result.ActualTools = append(result.ActualTools, run.Tool)
	}
	result.ActualIntent, _ = decoded.Metadata["intent_type"].(string)
	for _, evidence := range decoded.Answer.Evidence {
		result.ActualEvidence = append(result.ActualEvidence, evidence.ID)
	}
	result.FinalStatus = "completed"
	for _, limitation := range decoded.Answer.Limitations {
		if limitation.Code == "CLARIFICATION_REQUIRED" {
			result.FinalStatus = "clarified"
		}
	}
	if expectedDecision == "clarify" && result.FinalStatus != "clarified" {
		result.FinalStatus = "failed"
		result.FailureReason = "expected clarification"
	}
	if expectedIntent != "" && result.ActualIntent != expectedIntent {
		result.FinalStatus = "failed"
		result.FailureReason = "intent mismatch"
	}
	if result.FinalStatus == "completed" && !containsRequiredTools(result.ActualTools, expectedTools) {
		result.FinalStatus = "failed"
		result.FailureReason = "required tool missing"
	}
	return result
}

func containsRequiredTools(actual, required []string) bool {
	seen := map[string]struct{}{}
	for _, value := range actual {
		seen[value] = struct{}{}
	}
	for _, value := range required {
		if _, ok := seen[value]; !ok {
			return false
		}
	}
	return true
}

func expectations(category string) (string, []string, string) {
	switch category {
	case "metrics":
		return "metrics_query", []string{"query_metrics"}, "proceed"
	case "logs":
		return "logs_query", []string{"query_logs"}, "proceed"
	case "trace":
		return "trace_analysis", []string{"query_traces"}, "proceed"
	case "knowledge":
		return "knowledge_query", []string{"search_knowledge"}, "proceed"
	case "mitigation":
		return "mitigation_advice", []string{"search_knowledge"}, "proceed"
	case "incident":
		return "incident_triage", []string{"query_metrics", "query_logs", "search_knowledge"}, "proceed"
	case "clarification":
		return "", []string{}, "clarify"
	case "summary":
		return "status_summary", []string{}, "proceed"
	default:
		return "general_chat", []string{}, "proceed"
	}
}

func writeReport(value report) {
	if err := os.MkdirAll("data/eval", 0o755); err != nil {
		fatalf("create report directory: %v", err)
	}
	encoded, _ := json.MarshalIndent(value, "", "  ")
	if err := os.WriteFile("data/eval/agent_report.json", append(encoded, '\n'), 0o600); err != nil {
		fatalf("write report: %v", err)
	}
	var markdown strings.Builder
	fmt.Fprintf(&markdown, "# Agent E2E Evaluation\n\n- Enabled: `%t`\n- Planned cases: %d\n- Executed cases: %d\n", value.Enabled, value.RequestedCases, value.ExecutedCases)
	if value.SkippedReason != "" {
		fmt.Fprintf(&markdown, "- Skipped: %s\n", value.SkippedReason)
	}
	if err := os.WriteFile(filepath.Join("data", "eval", "agent_report.md"), []byte(markdown.String()), 0o600); err != nil {
		fatalf("write markdown: %v", err)
	}
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	return value
}
func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "agent eval failed: "+format+"\n", args...)
	os.Exit(1)
}
