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
	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/alerts"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/topology"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/traces"
	"go.opentelemetry.io/otel/attribute"
)

type AgentRunner interface {
	Run(ctx context.Context, input AgentInput) (AgentOutput, error)
}

// PromptRenderingRunner lets the application graph render a native Eino
// prompt before invoking an Agent. Deterministic runners intentionally do not
// implement it because they do not use a model prompt.
type PromptRenderingRunner interface {
	RenderPrompt(ctx context.Context, input AgentInput) ([]*schema.Message, error)
	RunPrepared(
		ctx context.Context,
		input AgentInput,
		messages []*schema.Message,
	) (AgentOutput, error)
}

type AgentInput struct {
	SessionSummary            session.Summary
	RecentMessages            []session.Message
	ConfirmedLongTermMemories []string
	DiagnosticSkills          []string
	UserProfileContext        []string
	RetrievedKnowledge        []string
	CurrentMessage            string
	TimeContext               common.TimeRange
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
	ctx, span := observability.StartSpan(
		ctx,
		"agent.run",
		attribute.String("runner.type", "deterministic"),
		attribute.Int("recent_message_count", len(input.RecentMessages)),
		attribute.Bool(
			"has_summary",
			input.SessionSummary.Version > 0 || input.SessionSummary.Content != "",
		),
		attribute.String("time_context.from", input.TimeContext.From),
		attribute.String("time_context.to", input.TimeContext.To),
	)
	output := AgentOutput{
		Conclusions:     []Conclusion{},
		Evidence:        []common.EvidenceItem{},
		Inferences:      []Inference{},
		Recommendations: []Recommendation{},
		Limitations:     []Limitation{},
		ToolRuns:        []ToolRun{},
		Metadata: map[string]any{
			"agent_mode":        "deterministic",
			"fallback_used":     false,
			"response_language": responseLanguage(input.CurrentMessage),
			"session_context_loaded": len(input.RecentMessages) > 0 ||
				input.SessionSummary.Version > 0 ||
				input.SessionSummary.Content != "",
			"recent_message_count": len(input.RecentMessages),
			"summary_version":      input.SessionSummary.Version,
		},
	}
	defer func() {
		span.SetAttributes(
			attribute.Int("tool_run_count", len(output.ToolRuns)),
			attribute.Int("evidence_count", len(output.Evidence)),
			attribute.Int("limitation_count", len(output.Limitations)),
		)
		span.End()
	}()

	calls := planToolCalls(input)
	chinese := prefersChinese(input.CurrentMessage)
	selectedTools := make([]string, 0, len(calls))
	for _, call := range calls {
		selectedTools = append(selectedTools, call.name)
	}
	span.SetAttributes(attribute.StringSlice("selected_tools", selectedTools))
	if len(calls) == 0 {
		output.Limitations = append(output.Limitations, Limitation{
			Code: "MORE_CONTEXT_REQUIRED",
			Message: localizedText(
				chinese,
				"No deterministic tool route matched the request; provide an error-rate, latency, trace, runbook, alert, or topology question.",
				"确定性降级路径未匹配到工具；请提供错误率、延迟、Trace、runbook、告警或拓扑相关问题。",
			),
		})
		return output, nil
	}

	successfulTools := make(map[string][]string, len(calls))
	mockEvidenceUsed := false
	evidenceCandidates := make([]evidence.Candidate, 0, len(calls))
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

		for _, item := range result.Evidence {
			evidenceCandidates = append(evidenceCandidates, evidence.Candidate{
				Item:      common.ToEvidenceItem(item),
				LatencyMS: result.DurationMS,
			})
		}
		if mode, ok := result.Metadata["mode"].(string); ok &&
			strings.HasPrefix(mode, "mock") {
			mockEvidenceUsed = true
		}
		evidenceIDs := collectEvidenceIDs(result.Evidence)
		if len(evidenceIDs) == 0 {
			continue
		}
		successfulTools[call.name] = evidenceIDs
		output.Conclusions = append(
			output.Conclusions,
			conclusionFor(call.name, evidenceIDs, chinese),
		)
		output.Recommendations = append(
			output.Recommendations,
			recommendationFor(call.name, evidenceIDs, chinese),
		)
	}
	output.Evidence, output.Metadata["evidence_groups"] = aggregateAgentEvidence(
		evidenceCandidates,
	)

	if metricEvidence, metricsOK := successfulTools[metrics.Name]; metricsOK {
		if logEvidence, logsOK := successfulTools[logs.Name]; logsOK {
			evidenceIDs := append(append([]string{}, metricEvidence...), logEvidence...)
			output.Inferences = append(output.Inferences, Inference{
				Text: localizedText(
					chinese,
					"The correlated metric and log evidence suggests upstream timeouts are a plausible contributor to the elevated error rate.",
					"关联的指标与日志证据表明，上游超时可能是错误率升高的影响因素。",
				),
				EvidenceIDs: evidenceIDs,
			})
		}
	}

	if mockEvidenceUsed {
		output.Limitations = append(output.Limitations, Limitation{
			Code: "MOCK_DATA",
			Message: localizedText(
				chinese,
				"This response includes deterministic mock evidence for one or more tools and is not a production-only investigation.",
				"本回答包含一个或多个工具的确定性 mock 证据，不能视为纯生产环境调查结果。",
			),
		})
	}

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

	EmitStreamEvent(ctx, "tool_call_started", map[string]any{"tool": call.name})
	rawResult, err := assembledTool.InvokableRun(ctx, string(arguments))
	run.DurationMS = time.Since(startedAt).Milliseconds()
	if err != nil {
		toolErr := safeToolError(call.name, err)
		run.ErrorCode = toolErr.Code
		EmitStreamEvent(ctx, "tool_call_failed", map[string]any{
			"tool":       call.name,
			"error_code": string(toolErr.Code),
			"latency_ms": run.DurationMS,
		})
		return common.ToolResult{}, run, toolErr
	}
	emitToolResultStreamEvent(ctx, call.name, rawResult)

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
		EmitStreamEvent(ctx, "tool_call_failed", map[string]any{
			"tool":       call.name,
			"error_code": string(toolErr.Code),
			"latency_ms": run.DurationMS,
		})
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
	calls := make([]plannedToolCall, 0, 4)
	logKeywords := []string{"error", "timeout"}
	if prefersChinese(input.CurrentMessage) {
		logKeywords = append(logKeywords, "错误", "超时")
	}

	if strings.Contains(message, "alert") ||
		strings.Contains(message, "firing") ||
		strings.Contains(message, "告警") {
		calls = append(calls, plannedToolCall{
			name: alerts.Name,
			arguments: alerts.Input{
				Service: service,
				Window:  "30m",
			},
		})
	}

	if strings.Contains(message, "topology") ||
		strings.Contains(message, "dependency") ||
		strings.Contains(message, "dependencies") ||
		strings.Contains(message, "拓扑") ||
		strings.Contains(message, "依赖") {
		calls = append(calls, plannedToolCall{
			name: topology.Name,
			arguments: topology.Input{
				Service: service,
				Depth:   1,
			},
		})
	}

	if strings.Contains(message, "error rate") ||
		strings.Contains(message, "错误率") {
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
					Keywords:  logKeywords,
					Level:     "error",
				},
			},
		)
	}

	if strings.Contains(message, "trace") ||
		strings.Contains(message, "slow") ||
		strings.Contains(message, "链路") ||
		strings.Contains(message, "慢调用") {
		calls = append(calls, plannedToolCall{
			name: traces.Name,
			arguments: traces.Input{
				Service:   service,
				TimeRange: input.TimeContext,
				TraceID:   inferTraceID(message),
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

func inferTraceID(message string) string {
	parts := strings.FieldsFunc(message, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
	for _, part := range parts {
		if len(part) != 32 {
			continue
		}
		valid := true
		for _, character := range part {
			if !((character >= '0' && character <= '9') ||
				(character >= 'a' && character <= 'f')) {
				valid = false
				break
			}
		}
		if valid {
			return part
		}
	}
	return ""
}

func inferService(message string) string {
	knownServices := []string{"checkout", "payment", "redis", "mysql"}
	selected := ""
	selectedIndex := len(message) + 1
	for _, service := range knownServices {
		if index := strings.Index(message, service); index >= 0 && index < selectedIndex {
			selected = service
			selectedIndex = index
		}
	}
	if selected != "" {
		return selected
	}

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
		case "alert", "firing", "topology", "dependency", "dependencies", "error", "slow", "trace", "latency":
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
		strings.Contains(message, "how do") ||
		strings.Contains(message, "知识") ||
		strings.Contains(message, "排查") ||
		strings.Contains(message, "怎么处理") ||
		strings.Contains(message, "如何处理")
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

func conclusionFor(
	toolName string,
	evidenceIDs []string,
	chinese bool,
) Conclusion {
	text := "The tool returned reliability evidence."
	switch toolName {
	case metrics.Name:
		text = localizedText(
			chinese,
			"Metric evidence reports the requested service reliability signal.",
			"指标证据展示了请求的服务可靠性信号。",
		)
	case logs.Name:
		text = localizedText(
			chinese,
			"Log evidence contains events matching the requested service and time range.",
			"日志证据包含与目标服务和时间范围匹配的事件。",
		)
	case traces.Name:
		text = localizedText(
			chinese,
			"Trace evidence identifies the slowest or error-marked spans.",
			"Trace 证据标识了最慢或带错误标记的 span。",
		)
	case knowledge.Name:
		text = localizedText(
			chinese,
			"Knowledge evidence contains a relevant diagnostic procedure.",
			"知识库证据包含相关排障流程。",
		)
	case alerts.Name:
		text = localizedText(
			chinese,
			"Alert evidence reports active or recent alert signals for the service.",
			"告警证据展示了该服务当前或近期的告警信号。",
		)
	case topology.Name:
		text = localizedText(
			chinese,
			"Topology evidence describes service dependencies relevant to the incident.",
			"拓扑证据描述了与本次故障相关的服务依赖。",
		)
	default:
		text = localizedText(chinese, text, "工具返回了可靠性证据。")
	}
	return Conclusion{Text: text, EvidenceIDs: evidenceIDs}
}

func recommendationFor(
	toolName string,
	evidenceIDs []string,
	chinese bool,
) Recommendation {
	text := "Review the cited evidence before taking action."
	switch toolName {
	case metrics.Name:
		text = localizedText(
			chinese,
			"Compare the affected interval with the service baseline and dependency metrics.",
			"将故障时间范围与服务基线及依赖指标进行对比。",
		)
	case logs.Name:
		text = localizedText(
			chinese,
			"Inspect upstream dependency saturation and timeout configuration.",
			"检查上游依赖饱和度和超时配置。",
		)
	case traces.Name:
		text = localizedText(
			chinese,
			"Inspect the cited slow span and its downstream dependency.",
			"检查引用的慢 span 及其下游依赖。",
		)
	case knowledge.Name:
		text = localizedText(
			chinese,
			"Follow the cited runbook steps and verify each action against live evidence.",
			"按照引用的 runbook 执行，并用实时证据验证每个操作。",
		)
	case alerts.Name:
		text = localizedText(
			chinese,
			"Compare the alert state with metrics, logs, and traces before escalating.",
			"升级处理前，将告警状态与指标、日志和 Trace 进行对比。",
		)
	case topology.Name:
		text = localizedText(
			chinese,
			"Use the dependency context to decide which upstream or downstream evidence to inspect next.",
			"根据依赖上下文决定下一步检查哪个上游或下游证据。",
		)
	default:
		text = localizedText(chinese, text, "采取操作前请检查引用的证据。")
	}
	return Recommendation{Text: text, EvidenceIDs: evidenceIDs}
}
