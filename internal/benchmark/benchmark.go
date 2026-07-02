package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

type Case struct {
	ID               string `json:"id"`
	SessionID        string `json:"session_id"`
	Message          string `json:"message"`
	ExpectedBehavior string `json:"expected_behavior"`
	Notes            string `json:"notes"`
}

type CaseResult struct {
	ID                string  `json:"id"`
	SessionID         string  `json:"session_id"`
	Success           bool    `json:"success"`
	HTTPStatus        int     `json:"http_status"`
	LatencyMS         float64 `json:"latency_ms"`
	ToolRuns          int     `json:"tool_runs"`
	EvidenceCount     int     `json:"evidence_count"`
	FallbackDetected  bool    `json:"fallback_detected"`
	LimitationCount   int     `json:"limitation_count"`
	RequestIDPresent  bool    `json:"request_id_present"`
	TraceIDPresent    bool    `json:"trace_id_present"`
	FailureController bool    `json:"failure_controller_detected,omitempty"`
	Error             string  `json:"error,omitempty"`
}

type Summary struct {
	TotalRequests             int     `json:"total_requests"`
	SuccessfulRequests        int     `json:"successful_requests"`
	FailedRequests            int     `json:"failed_requests"`
	SuccessRate               float64 `json:"success_rate"`
	AverageLatencyMS          float64 `json:"average_latency_ms"`
	P95LatencyMS              float64 `json:"p95_latency_ms"`
	MinLatencyMS              float64 `json:"min_latency_ms"`
	MaxLatencyMS              float64 `json:"max_latency_ms"`
	AverageToolRuns           float64 `json:"average_tool_runs"`
	AverageEvidenceCount      float64 `json:"average_evidence_count"`
	FallbackCount             int     `json:"fallback_count"`
	LimitationCount           int     `json:"limitation_count"`
	EmptyEvidenceCount        int     `json:"empty_evidence_count"`
	RequestIDPresenceRate     float64 `json:"request_id_presence_rate"`
	TraceIDPresenceRate       float64 `json:"trace_id_presence_rate"`
	FailureControllerHitCount int     `json:"failure_controller_trigger_count,omitempty"`
}

type StreamCheck struct {
	Attempted           bool   `json:"attempted"`
	Success             bool   `json:"success"`
	EventCount          int    `json:"stream_event_count"`
	FinalAnswerReceived bool   `json:"final_answer_received"`
	Error               string `json:"error,omitempty"`
}

type Report struct {
	GeneratedAt time.Time    `json:"generated_at"`
	BaseURL     string       `json:"base_url"`
	Cases       []CaseResult `json:"cases"`
	Summary     Summary      `json:"summary"`
	Stream      StreamCheck  `json:"stream_check"`
}

func LoadCases(reader io.Reader) ([]Case, error) {
	var cases []Case
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		return nil, fmt.Errorf("decode benchmark cases: %w", err)
	}
	if len(cases) == 0 {
		return nil, errors.New("benchmark cases must not be empty")
	}
	seen := make(map[string]struct{}, len(cases))
	for index, item := range cases {
		item.ID = strings.TrimSpace(item.ID)
		item.SessionID = strings.TrimSpace(item.SessionID)
		item.Message = strings.TrimSpace(item.Message)
		item.ExpectedBehavior = strings.TrimSpace(item.ExpectedBehavior)
		if item.ID == "" || item.SessionID == "" || item.Message == "" || item.ExpectedBehavior == "" {
			return nil, fmt.Errorf("benchmark case %d requires id, session_id, message, and expected_behavior", index)
		}
		if _, exists := seen[item.ID]; exists {
			return nil, fmt.Errorf("duplicate benchmark case id %q", item.ID)
		}
		seen[item.ID] = struct{}{}
		cases[index] = item
	}
	return cases, nil
}

func CalculateSummary(results []CaseResult) Summary {
	summary := Summary{TotalRequests: len(results)}
	if len(results) == 0 {
		return summary
	}

	latencies := make([]float64, 0, len(results))
	var totalToolRuns, totalEvidence, requestIDs, traceIDs int
	for _, result := range results {
		if result.Success {
			summary.SuccessfulRequests++
		} else {
			summary.FailedRequests++
		}
		latencies = append(latencies, result.LatencyMS)
		totalToolRuns += result.ToolRuns
		totalEvidence += result.EvidenceCount
		summary.LimitationCount += result.LimitationCount
		if result.FallbackDetected {
			summary.FallbackCount++
		}
		if result.EvidenceCount == 0 {
			summary.EmptyEvidenceCount++
		}
		if result.RequestIDPresent {
			requestIDs++
		}
		if result.TraceIDPresent {
			traceIDs++
		}
		if result.FailureController {
			summary.FailureControllerHitCount++
		}
	}

	count := float64(len(results))
	summary.SuccessRate = float64(summary.SuccessfulRequests) / count
	summary.AverageToolRuns = float64(totalToolRuns) / count
	summary.AverageEvidenceCount = float64(totalEvidence) / count
	summary.RequestIDPresenceRate = float64(requestIDs) / count
	summary.TraceIDPresenceRate = float64(traceIDs) / count

	sort.Float64s(latencies)
	for _, latency := range latencies {
		summary.AverageLatencyMS += latency
	}
	summary.AverageLatencyMS /= count
	summary.MinLatencyMS = latencies[0]
	summary.MaxLatencyMS = latencies[len(latencies)-1]
	summary.P95LatencyMS = PercentileNearestRank(latencies, 0.95)
	return summary
}

func PercentileNearestRank(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 1 {
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	return sorted[rank]
}
