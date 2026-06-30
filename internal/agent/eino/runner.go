package eino

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/traces"
)

type AgentRunner interface {
	Run(ctx context.Context, input AgentInput) (AgentOutput, error)
}

type AgentInput struct {
	SessionSummary session.Summary
	RecentMessages []session.Message
	CurrentMessage string
	TimeContext    common.TimeRange
}

type AgentOutput struct {
	Conclusions     []Conclusion
	Evidence        []common.EvidenceItem
	Inferences      []Inference
	Recommendations []Recommendation
	Limitations     []Limitation
	ToolRuns        []ToolRun
	Metadata        map[string]any
}

type Conclusion struct {
	Text        string
	EvidenceIDs []string
}

type Inference struct {
	Text        string
	EvidenceIDs []string
}

type Recommendation struct {
	Text        string
	EvidenceIDs []string
}

type Limitation struct {
	Code    string
	Message string
	Tool    string
}

type ToolRun struct {
	Tool          string
	Success       bool
	DurationMS    int64
	ErrorCode     common.ToolErrorCode
	EvidenceCount int
	WarningCount  int
}

type DeterministicRunner struct {
	tools []einotool.InvokableTool
}

type plannedToolCall struct {
	name      string
	arguments any
}

func NewDeterministicRunner(tools []einotool.InvokableTool) *DeterministicRunner {
	return &DeterministicRunner{tools: tools}
}

func (r *DeterministicRunner) Run(ctx context.Context, input AgentInput) (AgentOutput, error) {
	output := AgentOutput{
		Conclusions:     []Conclusion{},
		Evidence:        []common.EvidenceItem{},
		Inferences:      []Inference{},
		Recommendations: []Recommendation{},
		Limitations:     []Limitation{},
		ToolRuns:        []ToolRun{},
		Metadata: map[string]any{
			"session_context_loaded": len(input.RecentMessages) > 0 ||
				input.SessionSummary.Version > 0 ||
				input.SessionSummary.Content != "",
			"recent_message_count": len(input.RecentMessages),
			"summary_version":      input.SessionSummary.Version,
		},
	}

	calls := planToolCalls(input)
	if len(calls) == 0 {
		output.Limitations = append(output.Limitations, Limitation{
			Code:    "MORE_CONTEXT_REQUIRED",
			Message: "No deterministic tool route matched the request; provide an error-rate, latency, trace, or runbook question.",
		})
		return output, nil
	}

	successfulTools := make(map[string][]string, len(calls))
	for _, call := range calls {
		result, run, toolErr := r.invoke(ctx, call)
		output.ToolRuns = append(output.ToolRuns, run)

		if toolErr != nil {
			output.Limitations = append(output.Limitations, Limitation{
				Code:    string(toolErr.Code),
				Message: toolErr.Message,
				Tool:    call.name,
			})
			continue
		}

		output.Evidence = append(output.Evidence, result.Evidence...)
		evidenceIDs := collectEvidenceIDs(result.Evidence)
		successfulTools[call.name] = evidenceIDs
		output.Conclusions = append(output.Conclusions, conclusionFor(call.name, evidenceIDs))
		output.Recommendations = append(output.Recommendations, recommendationFor(call.name, evidenceIDs))
	}

	if metricEvidence, metricsOK := successfulTools[metrics.Name]; metricsOK {
		if logEvidence, logsOK := successfulTools[logs.Name]; logsOK {
			evidenceIDs := append(append([]string{}, metricEvidence...), logEvidence...)
			output.Inferences = append(output.Inferences, Inference{
				Text:        "The correlated mock metrics and logs suggest upstream timeouts are a plausible contributor to the elevated error rate.",
				EvidenceIDs: evidenceIDs,
			})
		}
	}

	output.Limitations = append(output.Limitations, Limitation{
		Code:    "MOCK_DATA",
		Message: "This response uses deterministic mock evidence and has not queried production systems.",
	})

	return output, nil
}

func (r *DeterministicRunner) invoke(
	ctx context.Context,
	call plannedToolCall,
) (common.ToolResult, ToolRun, *common.ToolError) {
	startedAt := time.Now()
	run := ToolRun{Tool: call.name}

	assembledTool, err := r.findTool(ctx, call.name)
	if err != nil {
		toolErr := common.NewToolError(
			common.ErrorCodeInternal,
			call.name,
			"tool is not available",
			false,
			nil,
			"continue with the remaining evidence",
		)
		run.DurationMS = time.Since(startedAt).Milliseconds()
		run.ErrorCode = toolErr.Code
		return common.ToolResult{}, run, toolErr
	}

	arguments, err := json.Marshal(call.arguments)
	if err != nil {
		toolErr := common.NewToolError(
			common.ErrorCodeInternal,
			call.name,
			"tool arguments could not be prepared",
			false,
			nil,
			"continue with the remaining evidence",
		)
		run.DurationMS = time.Since(startedAt).Milliseconds()
		run.ErrorCode = toolErr.Code
		return common.ToolResult{}, run, toolErr
	}

	rawResult, err := assembledTool.InvokableRun(ctx, string(arguments))
	run.DurationMS = time.Since(startedAt).Milliseconds()
	if err != nil {
		toolErr := safeToolError(call.name, err)
		run.ErrorCode = toolErr.Code
		return common.ToolResult{}, run, toolErr
	}

	var result common.ToolResult
	if err := json.Unmarshal([]byte(rawResult), &result); err != nil {
		toolErr := common.NewToolError(
			common.ErrorCodeInternal,
			call.name,
			"tool returned an invalid result",
			false,
			nil,
			"continue with the remaining evidence",
		)
		run.ErrorCode = toolErr.Code
		return common.ToolResult{}, run, toolErr
	}

	run.Success = result.Success
	run.DurationMS = result.DurationMS
	run.EvidenceCount = len(result.Evidence)
	run.WarningCount = len(result.Warnings)
	if result.Error != nil {
		run.ErrorCode = result.Error.Code
		return common.ToolResult{}, run, result.Error
	}

	return result, run, nil
}

func (r *DeterministicRunner) findTool(ctx context.Context, name string) (einotool.InvokableTool, error) {
	for _, candidate := range r.tools {
		info, err := candidate.Info(ctx)
		if err != nil {
			continue
		}
		if info.Name == name {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("tool not found")
}

func planToolCalls(input AgentInput) []plannedToolCall {
	message := strings.ToLower(strings.TrimSpace(input.CurrentMessage))
	service := inferService(message)
	calls := make([]plannedToolCall, 0, 3)

	if strings.Contains(message, "error rate") {
		calls = append(calls,
			plannedToolCall{
				name: metrics.Name,
				arguments: metrics.Input{
					Service:    service,
					MetricName: "http_server_error_rate",
					TimeRange:  input.TimeContext,
				},
			},
			plannedToolCall{
				name: logs.Name,
				arguments: logs.Input{
					Service:   service,
					TimeRange: input.TimeContext,
					Keywords:  []string{"error", "timeout"},
					Level:     "error",
				},
			},
		)
	}

	if strings.Contains(message, "trace") || strings.Contains(message, "slow") {
		calls = append(calls, plannedToolCall{
			name: traces.Name,
			arguments: traces.Input{
				Service:   service,
				TimeRange: input.TimeContext,
			},
		})
	}

	if isKnowledgeQuestion(message) {
		calls = append(calls, plannedToolCall{
			name: knowledge.Name,
			arguments: knowledge.Input{
				Query: message,
				TopK:  3,
			},
		})
	}

	return calls
}

func inferService(message string) string {
	words := strings.FieldsFunc(message, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})

	for index, word := range words {
		if word == "service" && index > 0 && isServiceCandidate(words[index-1]) {
			return words[index-1]
		}
	}

	for index, word := range words {
		switch word {
		case "error", "slow", "trace", "latency":
			if index > 0 && isServiceCandidate(words[index-1]) {
				return words[index-1]
			}
		}
	}

	return ""
}

func isServiceCandidate(value string) bool {
	switch value {
	case "", "a", "an", "did", "for", "in", "is", "last", "me", "show", "the", "what", "why":
		return false
	default:
		return true
	}
}

func isKnowledgeQuestion(message string) bool {
	return strings.Contains(message, "runbook") ||
		strings.Contains(message, "playbook") ||
		strings.Contains(message, "knowledge") ||
		strings.Contains(message, "how should") ||
		strings.Contains(message, "how do")
}

func safeToolError(toolName string, err error) *common.ToolError {
	var toolErr *common.ToolError
	if errors.As(err, &toolErr) {
		copy := *toolErr
		return &copy
	}
	return common.NewToolError(
		common.ErrorCodeInternal,
		toolName,
		"tool execution failed",
		false,
		nil,
		"continue with the remaining evidence",
	)
}

func collectEvidenceIDs(evidence []common.EvidenceItem) []string {
	ids := make([]string, 0, len(evidence))
	for _, item := range evidence {
		if item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func conclusionFor(toolName string, evidenceIDs []string) Conclusion {
	text := "The tool returned reliability evidence."
	switch toolName {
	case metrics.Name:
		text = "Mock metrics report elevated latency and error rate."
	case logs.Name:
		text = "Mock logs report repeated upstream timeout errors."
	case traces.Name:
		text = "Mock trace evidence identifies a slow database span."
	case knowledge.Name:
		text = "A mock runbook contains a relevant diagnostic procedure."
	}
	return Conclusion{Text: text, EvidenceIDs: evidenceIDs}
}

func recommendationFor(toolName string, evidenceIDs []string) Recommendation {
	text := "Review the cited evidence before taking action."
	switch toolName {
	case metrics.Name:
		text = "Compare the affected interval with the service baseline and dependency metrics."
	case logs.Name:
		text = "Inspect upstream dependency saturation and timeout configuration."
	case traces.Name:
		text = "Inspect the cited slow span and its downstream dependency."
	case knowledge.Name:
		text = "Follow the cited runbook steps and verify each action against live evidence."
	}
	return Recommendation{Text: text, EvidenceIDs: evidenceIDs}
}
