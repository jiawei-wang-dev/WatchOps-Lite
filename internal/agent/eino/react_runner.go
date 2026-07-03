package eino

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/control"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

type ReActRunnerConfig struct {
	MaxIterations int
	Timeout       time.Duration
	PromptVersion string
	ModelName     string
	Control       control.Config
}

type reactGenerator interface {
	Generate(context.Context, []*schema.Message, ...agent.AgentOption) (*schema.Message, error)
}

type ReActRunner struct {
	agent     reactGenerator
	prompt    *PromptBuilder
	config    ReActRunnerConfig
	toolNames []string
	toolCount int
	control   *control.Controller
}

func NewReActRunner(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	tools []einotool.InvokableTool,
	config ReActRunnerConfig,
) (*ReActRunner, error) {
	if chatModel == nil {
		return nil, errors.New("Eino ReAct ChatModel is required")
	}
	if config.MaxIterations <= 0 {
		return nil, errors.New("Eino ReAct max iterations must be positive")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("Eino ReAct timeout must be positive")
	}
	promptBuilder, err := NewPromptBuilder(config.PromptVersion)
	if err != nil {
		return nil, err
	}
	if control.IsZero(config.Control) {
		config.Control = control.DefaultConfig()
	}

	baseTools := make([]einotool.BaseTool, 0, len(tools))
	toolNames := make([]string, 0, len(tools))
	for _, currentTool := range tools {
		info, err := currentTool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read Eino tool info: %w", err)
		}
		baseTools = append(baseTools, &agentTool{
			delegate: currentTool,
			name:     info.Name,
		})
		toolNames = append(toolNames, info.Name)
	}

	tracedChatModel := &tracedToolCallingModel{
		delegate:      chatModel,
		modelName:     config.ModelName,
		promptVersion: config.PromptVersion,
	}
	reactAgent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: tracedChatModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: baseTools,
		},
		MaxStep: config.MaxIterations*2 + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("build Eino ReAct Agent: %w", err)
	}
	return &ReActRunner{
		agent:     reactAgent,
		prompt:    promptBuilder,
		config:    config,
		toolNames: toolNames,
		toolCount: len(toolNames),
		control:   control.New(config.Control),
	}, nil
}

func (r *ReActRunner) Run(ctx context.Context, input AgentInput) (AgentOutput, error) {
	runContext, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()
	messages, err := r.RenderPrompt(runContext, input)
	if err != nil {
		return AgentOutput{}, err
	}
	return r.runPrepared(runContext, input, messages)
}

func (r *ReActRunner) RenderPrompt(
	ctx context.Context,
	input AgentInput,
) ([]*schema.Message, error) {
	return r.prompt.Build(ctx, input)
}

func (r *ReActRunner) RunPrepared(
	ctx context.Context,
	input AgentInput,
	messages []*schema.Message,
) (AgentOutput, error) {
	runContext, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()
	return r.runPrepared(runContext, input, messages)
}

func (r *ReActRunner) runPrepared(
	ctx context.Context,
	input AgentInput,
	messages []*schema.Message,
) (AgentOutput, error) {
	startedAt := time.Now()
	ctx, span := observability.StartSpan(
		ctx,
		"agent.eino.run",
		attribute.String("agent.mode", "eino_react"),
		attribute.String("prompt_version", r.config.PromptVersion),
		attribute.String("model", r.config.ModelName),
		attribute.Int("max_iterations", r.config.MaxIterations),
		attribute.Int("tool_count", r.toolCount),
		attribute.StringSlice("tool_names", r.toolNames),
		attribute.Bool("fallback_used", false),
	)
	defer span.End()

	futureOption, future := react.WithMessageFuture()
	finalMessage, err := r.agent.Generate(ctx, messages, futureOption)
	if err != nil {
		observability.MarkError(span, "Eino ReAct execution failed")
		return AgentOutput{}, fmt.Errorf("Eino ReAct execution failed")
	}
	evidenceItems, evidenceGroups, toolRuns, toolLimitations := collectToolResults(
		future.GetMessages(),
	)
	executedToolNames := make([]string, 0, len(toolRuns))
	for _, run := range toolRuns {
		executedToolNames = append(executedToolNames, run.Tool)
	}
	content := ""
	if finalMessage != nil {
		content = finalMessage.Content
	}

	_, parseSpan := observability.StartSpan(
		ctx,
		"agent.output.parse",
		attribute.String("prompt_version", r.config.PromptVersion),
	)
	output := parseAgentOutputControlled(
		ctx,
		r.control,
		content,
		evidenceItems,
		toolRuns,
		toolLimitations,
		map[string]any{
			"agent_mode":        "eino_react",
			"fallback_used":     false,
			"response_language": responseLanguage(input.CurrentMessage),
			"prompt_version":    r.config.PromptVersion,
			"model":             r.config.ModelName,
			"max_iterations":    r.config.MaxIterations,
			"tool_count":        len(toolRuns),
			"tool_names":        executedToolNames,
			"evidence_groups":   evidenceGroups,
			"session_context_loaded": len(input.RecentMessages) > 0 ||
				input.SessionSummary.Version > 0 ||
				input.SessionSummary.Content != "",
			"recent_message_count": len(input.RecentMessages),
			"summary_version":      input.SessionSummary.Version,
		},
	)
	parseSuccess, _ := output.Metadata["output_parse_success"].(bool)
	parseSpan.SetAttributes(attribute.Bool("output_parse_success", parseSuccess))
	if !parseSuccess {
		observability.MarkError(parseSpan, "Agent output parsing failed")
	}
	parseSpan.End()
	state := control.BuildState(
		controlToolRuns(toolRuns),
		len(output.Evidence),
		len(output.Limitations),
		control.Since(startedAt),
		parseSuccess,
		outputMetadataBool(output.Metadata, "missing_required_sections"),
		r.config.MaxIterations,
	)
	evaluation := r.control.Evaluate(ctx, state)
	appendControlLimitations(
		&output,
		evaluation.Limitations,
		prefersChinese(input.CurrentMessage),
	)
	if evaluation.FailureReason != "" {
		output.Metadata["failure_reason"] = evaluation.FailureReason
		output.Metadata["failure_controller_triggered"] = evaluation.Controlled
		output.Metadata["failure_controller_fallback_required"] = evaluation.ShouldFallback
	}
	span.SetAttributes(
		attribute.Bool("output_parse_success", parseSuccess),
		attribute.Int("evidence_count", len(output.Evidence)),
		attribute.Int("tool_run_count", len(output.ToolRuns)),
		attribute.String("failure_reason", evaluation.FailureReason),
	)
	return output, nil
}

func controlToolRuns(toolRuns []ToolRun) []control.ToolRun {
	result := make([]control.ToolRun, 0, len(toolRuns))
	for _, run := range toolRuns {
		result = append(result, control.ToolRun{
			Tool:          run.Tool,
			Success:       run.Success,
			ErrorCode:     string(run.ErrorCode),
			EvidenceCount: run.EvidenceCount,
		})
	}
	return result
}

func appendControlLimitations(
	output *AgentOutput,
	limitations []control.Limitation,
	chinese bool,
) {
	for _, limitation := range limitations {
		if hasLimitationCode(output.Limitations, limitation.Code) {
			continue
		}
		output.Limitations = append(output.Limitations, Limitation{
			Code: limitation.Code,
			Message: localizedControlMessage(
				limitation.Code,
				limitation.Message,
				chinese,
			),
			Tool: limitation.Tool,
		})
	}
}

func localizedControlMessage(code, fallback string, chinese bool) string {
	if !chinese {
		return fallback
	}
	switch code {
	case "AGENT_OUTPUT_PARSE_FAILED":
		return "模型回答无法解析为要求的 JSON 结构。"
	case "AGENT_OUTPUT_MISSING_REQUIRED_SECTIONS":
		return "模型回答缺少一个或多个必需部分。"
	case "AGENT_MAX_TOOL_CALLS_EXCEEDED":
		return "Agent 超过了配置的最大工具调用次数。"
	case "AGENT_CONSECUTIVE_TOOL_FAILURES":
		return "多个工具连续失败，Agent 已停止继续执行高风险调用。"
	case "AGENT_REPEATED_TOOL_CALL":
		return "Agent 重复了相同工具调用，重复执行被视为低价值。"
	case "AGENT_MAX_ITERATIONS_REACHED":
		return "Agent 在获得更强证据前达到了最大迭代边界。"
	case "AGENT_TOTAL_EXECUTION_TIMEOUT":
		return "Agent 超过了配置的总执行超时。"
	case "INSUFFICIENT_EVIDENCE":
		return "没有工具证据支持已观察到的根因结论。"
	default:
		return fallback
	}
}

func hasLimitationCode(limitations []Limitation, code string) bool {
	for _, limitation := range limitations {
		if limitation.Code == code {
			return true
		}
	}
	return false
}

func outputMetadataBool(metadata map[string]any, key string) bool {
	value, _ := metadata[key].(bool)
	return value
}

func collectToolResults(iterator *react.Iterator[*schema.Message]) (
	[]common.EvidenceItem,
	map[string]int,
	[]ToolRun,
	[]Limitation,
) {
	candidates := []evidence.Candidate{}
	toolRuns := []ToolRun{}
	limitations := []Limitation{}
	for {
		message, hasNext, err := iterator.Next()
		if err != nil || !hasNext {
			break
		}
		if message == nil || message.Role != schema.Tool {
			continue
		}
		var result common.ToolResult
		if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
			toolRuns = append(toolRuns, ToolRun{
				Tool:      message.ToolName,
				ErrorCode: common.ErrorCodeInternal,
			})
			limitations = append(limitations, Limitation{
				Code:    string(common.ErrorCodeInternal),
				Message: "A tool returned an invalid result.",
				Tool:    message.ToolName,
			})
			continue
		}
		toolName := result.Tool
		if toolName == "" {
			toolName = message.ToolName
		}
		run := ToolRun{
			Tool:          toolName,
			Success:       result.Success,
			DurationMS:    result.DurationMS,
			EvidenceCount: len(result.Evidence),
			WarningCount:  len(result.Warnings),
		}
		if result.Error != nil {
			run.ErrorCode = result.Error.Code
			limitations = append(limitations, Limitation{
				Code:    string(result.Error.Code),
				Message: result.Error.Message,
				Tool:    toolName,
			})
		}
		toolRuns = append(toolRuns, run)
		for _, item := range result.Evidence {
			candidates = append(candidates, evidence.Candidate{
				Item:      common.ToEvidenceItem(item),
				LatencyMS: result.DurationMS,
			})
		}
	}
	evidenceItems, evidenceGroups := aggregateAgentEvidence(candidates)
	return evidenceItems, evidenceGroups, toolRuns, limitations
}

type agentTool struct {
	delegate einotool.InvokableTool
	name     string
}

func (t *agentTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.delegate.Info(ctx)
}

func (t *agentTool) InvokableRun(
	ctx context.Context,
	arguments string,
	options ...einotool.Option,
) (string, error) {
	EmitStreamEvent(ctx, "tool_call_started", map[string]any{
		"tool": t.name,
	})
	ctx, span := observability.StartSpan(
		ctx,
		"agent.tool_call",
		attribute.String("tool.name", t.name),
	)
	defer span.End()

	result, err := t.delegate.InvokableRun(ctx, arguments, options...)
	if err == nil {
		emitToolResultStreamEvent(ctx, t.name, result)
		return result, nil
	}
	observability.MarkError(span, "Agent tool call failed")
	toolError := safeToolError(t.name, err)
	failure := common.ToolResult{
		Tool:     t.name,
		Success:  false,
		Evidence: []common.EvidenceItem{},
		Warnings: []common.ToolWarning{},
		Metadata: map[string]any{},
		Error:    toolError,
	}
	encoded, marshalErr := json.Marshal(failure)
	if marshalErr != nil {
		EmitStreamEvent(ctx, "tool_call_failed", map[string]any{
			"tool":       t.name,
			"error_code": string(common.ErrorCodeInternal),
		})
		return "", fmt.Errorf("encode safe tool failure")
	}
	EmitStreamEvent(ctx, "tool_call_failed", map[string]any{
		"tool":       t.name,
		"error_code": string(toolError.Code),
	})
	return string(encoded), nil
}

func emitToolResultStreamEvent(ctx context.Context, toolName string, encoded string) {
	var result common.ToolResult
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		EmitStreamEvent(ctx, "tool_call_completed", map[string]any{
			"tool": toolName,
		})
		return
	}
	eventType := "tool_call_completed"
	data := map[string]any{
		"tool":           result.Tool,
		"evidence_count": len(result.Evidence),
		"latency_ms":     result.DurationMS,
	}
	if result.Tool == "" {
		data["tool"] = toolName
	}
	if len(result.Evidence) > 0 {
		data["source_type"] = result.Evidence[0].SourceType
	}
	if result.Error != nil {
		eventType = "tool_call_failed"
		data["error_code"] = string(result.Error.Code)
		if result.Error.Details != nil {
			if errorType, _ := result.Error.Details["error_type"].(string); errorType != "" {
				data["error_type"] = errorType
			}
		}
	}
	EmitStreamEvent(ctx, eventType, data)
}

type tracedToolCallingModel struct {
	delegate      model.ToolCallingChatModel
	modelName     string
	promptVersion string
}

func (m *tracedToolCallingModel) Generate(
	ctx context.Context,
	input []*schema.Message,
	options ...model.Option,
) (*schema.Message, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"agent.llm.call",
		attribute.String("model", m.modelName),
		attribute.String("prompt_version", m.promptVersion),
		attribute.Int("message_count", len(input)),
	)
	defer span.End()
	result, err := m.delegate.Generate(ctx, input, options...)
	if err != nil {
		observability.MarkError(span, "LLM call failed")
		return nil, err
	}
	if result != nil {
		span.SetAttributes(
			attribute.Int("tool_call_count", len(result.ToolCalls)),
			attribute.Int("output_length", len(result.Content)),
		)
	}
	return result, nil
}

func (m *tracedToolCallingModel) Stream(
	ctx context.Context,
	input []*schema.Message,
	options ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	ctx, span := observability.StartSpan(
		ctx,
		"agent.llm.stream",
		attribute.String("model", m.modelName),
		attribute.String("prompt_version", m.promptVersion),
	)
	result, err := m.delegate.Stream(ctx, input, options...)
	if err != nil {
		observability.MarkError(span, "LLM stream setup failed")
	}
	span.End()
	return result, err
}

func (m *tracedToolCallingModel) WithTools(
	tools []*schema.ToolInfo,
) (model.ToolCallingChatModel, error) {
	bound, err := m.delegate.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &tracedToolCallingModel{
		delegate:      bound,
		modelName:     m.modelName,
		promptVersion: m.promptVersion,
	}, nil
}

var _ model.ToolCallingChatModel = (*tracedToolCallingModel)(nil)
var _ einotool.InvokableTool = (*agentTool)(nil)
var _ PromptRenderingRunner = (*ReActRunner)(nil)
