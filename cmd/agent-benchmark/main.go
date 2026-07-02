package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/benchmark"
)

type chatRequest struct {
	SessionID   string      `json:"session_id"`
	Message     string      `json:"message"`
	TimeContext timeContext `json:"time_context"`
}

type timeContext struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type chatResponse struct {
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
	Answer    struct {
		Evidence []struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"evidence"`
		Limitations []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"limitations"`
	} `json:"answer"`
	ToolRuns []json.RawMessage `json:"tool_runs"`
	Metadata map[string]any    `json:"metadata"`
}

func main() {
	var (
		casesPath  = flag.String("cases", "testdata/agent_benchmark_cases.json", "path to Agent benchmark cases")
		baseURL    = flag.String("base-url", "http://localhost:8080", "WatchOps-Lite API base URL")
		timeout    = flag.Duration("timeout", 45*time.Second, "timeout for each request")
		outputJSON = flag.String(
			"output-json",
			"tmp/agent_benchmark_report.json",
			"JSON report output path; empty disables output",
		)
		outputMarkdown = flag.String(
			"output-markdown",
			"tmp/agent_benchmark_report.md",
			"Markdown report output path; empty disables output",
		)
		checkStream = flag.Bool("stream", true, "verify one SSE request and final_answer event")
	)
	flag.Parse()

	casesFile, err := os.Open(*casesPath)
	if err != nil {
		exitf("open cases: %v", err)
	}
	defer casesFile.Close()
	cases, err := benchmark.LoadCases(casesFile)
	if err != nil {
		exitf("%v", err)
	}

	client := &http.Client{Timeout: *timeout}
	normalizedBaseURL := strings.TrimRight(*baseURL, "/")
	if err := checkHealth(client, normalizedBaseURL); err != nil {
		exitf("%v", err)
	}

	generatedAt := time.Now().UTC()
	runSuffix := generatedAt.Format("20060102T150405")
	results := make([]benchmark.CaseResult, 0, len(cases))
	for _, item := range cases {
		results = append(results, runCase(client, normalizedBaseURL, item, runSuffix))
	}

	streamCheck := benchmark.StreamCheck{}
	if *checkStream {
		streamCheck = runStreamCheck(client, normalizedBaseURL, cases[0], runSuffix)
	}

	report := benchmark.Report{
		GeneratedAt: generatedAt,
		BaseURL:     normalizedBaseURL,
		Cases:       results,
		Summary:     benchmark.CalculateSummary(results),
		Stream:      streamCheck,
	}
	printReport(report)
	if *outputJSON != "" {
		if err := writeJSONReport(*outputJSON, report); err != nil {
			exitf("%v", err)
		}
	}
	if *outputMarkdown != "" {
		if err := writeMarkdownReport(*outputMarkdown, report); err != nil {
			exitf("%v", err)
		}
	}
	if report.Summary.FailedRequests > 0 || (streamCheck.Attempted && !streamCheck.Success) {
		os.Exit(1)
	}
}

func checkHealth(client *http.Client, baseURL string) error {
	response, err := client.Get(baseURL + "/healthz")
	if err != nil {
		return fmt.Errorf("WatchOps-Lite is not reachable at %s: %w", baseURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("WatchOps-Lite health check returned HTTP %d", response.StatusCode)
	}
	return nil
}

func runCase(
	client *http.Client,
	baseURL string,
	item benchmark.Case,
	runSuffix string,
) benchmark.CaseResult {
	now := time.Now().UTC()
	request := chatRequest{
		SessionID: item.SessionID + "-" + runSuffix,
		Message:   item.Message,
		TimeContext: timeContext{
			From: now.Add(-20 * time.Minute).Format(time.RFC3339),
			To:   now.Format(time.RFC3339),
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return benchmark.CaseResult{ID: item.ID, SessionID: request.SessionID, Error: err.Error()}
	}

	started := time.Now()
	response, err := client.Post(
		baseURL+"/api/v1/chat",
		"application/json",
		bytes.NewReader(body),
	)
	latencyMS := float64(time.Since(started).Microseconds()) / 1000
	result := benchmark.CaseResult{
		ID:        item.ID,
		SessionID: request.SessionID,
		LatencyMS: latencyMS,
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		result.Error = fmt.Sprintf("read response: %v", err)
		return result
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		result.Error = fmt.Sprintf("HTTP %d: %s", response.StatusCode, compactText(responseBody, 240))
		return result
	}

	var decoded chatResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		result.Error = fmt.Sprintf("decode response: %v", err)
		return result
	}
	result.Success = true
	result.ToolRuns = len(decoded.ToolRuns)
	result.EvidenceCount = len(decoded.Answer.Evidence)
	result.LimitationCount = len(decoded.Answer.Limitations)
	result.RequestIDPresent = strings.TrimSpace(decoded.RequestID) != ""
	result.TraceIDPresent = strings.TrimSpace(decoded.TraceID) != ""
	result.FallbackDetected = detectFallback(
		decoded.Metadata,
		decoded.Answer.Evidence,
		decoded.Answer.Limitations,
	)
	result.FailureController = detectMetadataKey(decoded.Metadata, "failure_controller")
	return result
}

func runStreamCheck(
	client *http.Client,
	baseURL string,
	item benchmark.Case,
	runSuffix string,
) benchmark.StreamCheck {
	result := benchmark.StreamCheck{Attempted: true}
	now := time.Now().UTC()
	payload, err := json.Marshal(chatRequest{
		SessionID: item.SessionID + "-stream-" + runSuffix,
		Message:   item.Message,
		TimeContext: timeContext{
			From: now.Add(-20 * time.Minute).Format(time.RFC3339),
			To:   now.Format(time.RFC3339),
		},
	})
	if err != nil {
		result.Error = err.Error()
		return result
	}

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		baseURL+"/api/v1/chat/stream",
		bytes.NewReader(payload),
	)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		result.Error = fmt.Sprintf("HTTP %d: %s", response.StatusCode, compactText(body, 240))
		return result
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "event:") {
			continue
		}
		result.EventCount++
		if strings.TrimSpace(strings.TrimPrefix(line, "event:")) == "final_answer" {
			result.FinalAnswerReceived = true
		}
	}
	if err := scanner.Err(); err != nil {
		result.Error = err.Error()
		return result
	}
	result.Success = result.FinalAnswerReceived
	if !result.Success {
		result.Error = "stream completed without final_answer"
	}
	return result
}

func detectFallback(
	metadata map[string]any,
	evidence []struct {
		Metadata map[string]any `json:"metadata"`
	},
	limitations []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	},
) bool {
	if detectMetadataKey(metadata, "fallback") {
		return true
	}
	for _, item := range evidence {
		if detectMetadataKey(item.Metadata, "fallback") {
			return true
		}
	}
	for _, limitation := range limitations {
		if strings.Contains(strings.ToLower(limitation.Code+" "+limitation.Message), "fallback") {
			return true
		}
	}
	return false
}

func detectMetadataKey(value any, keyFragment string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.Contains(strings.ToLower(key), keyFragment) && meaningfulValue(child) {
				return true
			}
			if detectMetadataKey(child, keyFragment) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if detectMetadataKey(child, keyFragment) {
				return true
			}
		}
	}
	return false
}

func meaningfulValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != ""
	case float64:
		return typed != 0
	default:
		return true
	}
}

func printReport(report benchmark.Report) {
	fmt.Println("WatchOps-Lite Agent benchmark")
	fmt.Printf("%-28s %-8s %12s %8s %10s %12s\n", "CASE", "STATUS", "LATENCY_MS", "TOOLS", "EVIDENCE", "LIMITATIONS")
	for _, result := range report.Cases {
		status := "PASS"
		if !result.Success {
			status = "FAIL"
		}
		fmt.Printf(
			"%-28s %-8s %12.2f %8d %10d %12d\n",
			result.ID,
			status,
			result.LatencyMS,
			result.ToolRuns,
			result.EvidenceCount,
			result.LimitationCount,
		)
	}
	for _, result := range report.Cases {
		if result.Error != "" {
			fmt.Printf("Failure %s: %s\n", result.ID, result.Error)
		}
	}
	fmt.Println()
	fmt.Printf("Requests: %d total, %d successful, %d failed (%.1f%% success)\n",
		report.Summary.TotalRequests,
		report.Summary.SuccessfulRequests,
		report.Summary.FailedRequests,
		report.Summary.SuccessRate*100,
	)
	fmt.Printf(
		"Latency ms: avg %.2f, p95 %.2f, min %.2f, max %.2f\n",
		report.Summary.AverageLatencyMS,
		report.Summary.P95LatencyMS,
		report.Summary.MinLatencyMS,
		report.Summary.MaxLatencyMS,
	)
	fmt.Printf(
		"Average tool runs: %.2f; average evidence: %.2f; fallbacks: %d; limitations: %d; empty evidence: %d\n",
		report.Summary.AverageToolRuns,
		report.Summary.AverageEvidenceCount,
		report.Summary.FallbackCount,
		report.Summary.LimitationCount,
		report.Summary.EmptyEvidenceCount,
	)
	fmt.Printf(
		"ID presence: request %.1f%%, trace %.1f%%\n",
		report.Summary.RequestIDPresenceRate*100,
		report.Summary.TraceIDPresenceRate*100,
	)
	if report.Stream.Attempted {
		fmt.Printf(
			"SSE check: success=%t, events=%d, final_answer=%t\n",
			report.Stream.Success,
			report.Stream.EventCount,
			report.Stream.FinalAnswerReceived,
		)
	}
}

func writeJSONReport(path string, report benchmark.Report) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON report: %w", err)
	}
	return writeFile(path, append(encoded, '\n'))
}

func writeMarkdownReport(path string, report benchmark.Report) error {
	var output strings.Builder
	fmt.Fprintf(&output, "# WatchOps-Lite Local Agent Benchmark\n\n")
	fmt.Fprintf(&output, "Generated: %s\n\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&output, "API: `%s`\n\n", report.BaseURL)
	fmt.Fprintf(&output, "| Case | Status | Latency (ms) | Tool runs | Evidence | Limitations | Fallback detected |\n")
	fmt.Fprintf(&output, "|---|---:|---:|---:|---:|---:|---:|\n")
	for _, result := range report.Cases {
		status := "pass"
		if !result.Success {
			status = "fail"
		}
		fmt.Fprintf(
			&output,
			"| %s | %s | %.2f | %d | %d | %d | %t |\n",
			escapeMarkdown(result.ID),
			status,
			result.LatencyMS,
			result.ToolRuns,
			result.EvidenceCount,
			result.LimitationCount,
			result.FallbackDetected,
		)
	}
	fmt.Fprintf(&output, "\n## Summary\n\n")
	fmt.Fprintf(&output, "- Requests: %d total, %d successful, %d failed\n",
		report.Summary.TotalRequests,
		report.Summary.SuccessfulRequests,
		report.Summary.FailedRequests,
	)
	fmt.Fprintf(&output, "- Success rate: %.1f%%\n", report.Summary.SuccessRate*100)
	fmt.Fprintf(&output, "- Latency: average %.2f ms, p95 %.2f ms, min %.2f ms, max %.2f ms\n",
		report.Summary.AverageLatencyMS,
		report.Summary.P95LatencyMS,
		report.Summary.MinLatencyMS,
		report.Summary.MaxLatencyMS,
	)
	fmt.Fprintf(&output, "- Average tool runs: %.2f\n", report.Summary.AverageToolRuns)
	fmt.Fprintf(&output, "- Average evidence count: %.2f\n", report.Summary.AverageEvidenceCount)
	fmt.Fprintf(&output, "- Detected fallbacks: %d\n", report.Summary.FallbackCount)
	fmt.Fprintf(&output, "- Limitations: %d; empty-evidence responses: %d\n",
		report.Summary.LimitationCount,
		report.Summary.EmptyEvidenceCount,
	)
	fmt.Fprintf(&output, "- Request ID presence: %.1f%%; trace ID presence: %.1f%%\n",
		report.Summary.RequestIDPresenceRate*100,
		report.Summary.TraceIDPresenceRate*100,
	)
	if report.Stream.Attempted {
		fmt.Fprintf(&output, "- SSE check: success `%t`, %d events, final answer received `%t`\n",
			report.Stream.Success,
			report.Stream.EventCount,
			report.Stream.FinalAnswerReceived,
		)
	}
	fmt.Fprintf(&output, "\nThis is a small local validation run, not a production load-test result.\n")
	return writeFile(path, []byte(output.String()))
}

func writeFile(path string, data []byte) error {
	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create report directory: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write report %s: %w", path, err)
	}
	return nil
}

func compactText(value []byte, limit int) string {
	text := strings.Join(strings.Fields(string(value)), " ")
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func escapeMarkdown(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Agent benchmark failed: "+format+"\n", args...)
	os.Exit(1)
}
