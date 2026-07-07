package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/alerts"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/topology"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/traces"
)

var evidenceToolBySource = map[string]string{
	"metrics":  metrics.Name,
	"logs":     logs.Name,
	"alerts":   alerts.Name,
	"traces":   traces.Name,
	"topology": topology.Name,
}

type EvidenceAgent struct {
	tools map[string]einotool.InvokableTool
	llm   *RoleLLM
}

func NewEvidenceAgent(
	ctx context.Context,
	tools []einotool.InvokableTool,
) (*EvidenceAgent, error) {
	available := make(map[string]einotool.InvokableTool, len(tools))
	for _, current := range tools {
		if current == nil {
			continue
		}
		info, err := current.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read Eino tool info: %w", err)
		}
		available[info.Name] = current
	}
	return &EvidenceAgent{tools: available}, nil
}

func (a *EvidenceAgent) WithLLM(llm *RoleLLM) *EvidenceAgent {
	a.llm = llm
	return a
}

func (a *EvidenceAgent) Analyze(
	ctx context.Context,
	plan TriagePlan,
) (AgentFinding, error) {
	// Evidence Agent is intentionally observation-only: it may summarize metrics,
	// logs, alerts, traces, and topology, but synthesis owns any diagnostic claim.
	finding := AgentFinding{
		Role:        AgentRoleEvidence,
		Evidence:    []common.EvidenceItem{},
		EvidenceIDs: []string{},
		ToolRuns:    []agenteino.ToolRun{},
		Limitations: []agenteino.Limitation{},
		Metadata: map[string]any{
			"planned_sources":  append([]string{}, plan.EvidencePlan...),
			"role_skill_cards": plan.AgentPlan.RoleSkillCards[AgentRoleEvidence],
			"role_skill_names": roleSkillNamesForRole(
				plan.AgentPlan.RoleSkillHints,
				AgentRoleEvidence,
			),
		},
	}
	executedSources := []string{}
	summaries := []string{}
	if chunks := plan.RoleRAG.ChunksByRole[AgentRoleEvidence]; len(chunks) > 0 {
		finding.Metadata["role_rag_chunk_count"] = len(chunks)
		summaries = append(
			summaries,
			"pre-rag: "+boundedSummary(chunks[0].Content, 180),
		)
	}
	for _, source := range plan.EvidencePlan {
		toolName, observationSource := evidenceToolBySource[source]
		if !observationSource {
			continue
		}
		executedSources = append(executedSources, source)
		result, run, limitation := a.invoke(
			ctx,
			toolName,
			evidenceArguments(source, plan),
			plan.Language,
		)
		finding.ToolRuns = append(finding.ToolRuns, run)
		if limitation != nil {
			finding.Limitations = append(finding.Limitations, *limitation)
			continue
		}
		if len(result.Evidence) == 0 {
			finding.Limitations = append(
				finding.Limitations,
				noEvidenceLimitation(source, toolName, plan.Language),
			)
			continue
		}
		finding.Evidence = append(finding.Evidence, result.Evidence...)
		for _, item := range result.Evidence {
			finding.EvidenceIDs = append(finding.EvidenceIDs, item.ID)
		}
		summaries = append(
			summaries,
			source+": "+boundedSummary(result.Evidence[0].Content, 180),
		)
	}
	finding.Metadata["executed_sources"] = executedSources
	finding.Metadata["evidence_count"] = len(finding.Evidence)
	finding.Summary = observationSummary(
		plan.Language,
		plan.Service,
		summaries,
		len(finding.Limitations),
	)
	finding.Metadata["evidence_llm_used"] = false
	finding.Metadata["evidence_llm_attempted"] = false
	finding.Metadata["evidence_model"] = ""
	finding.Metadata["evidence_fallback_used"] = true
	finding.Metadata["evidence_llm_duration_ms"] = int64(0)
	if a.llm != nil {
		finding.Metadata["evidence_llm_attempted"] = true
		analysis, call, err := a.llm.analyzeEvidence(
			ctx,
			plan,
			finding.Evidence,
			finding.Limitations,
		)
		finding.Metadata["evidence_model"] = a.llm.modelName
		finding.Metadata["evidence_llm_duration_ms"] = call.durationMS
		if err == nil {
			finding.Summary = strings.TrimSpace(analysis.ObservationSummary)
			finding.EvidenceIDs = append([]string{}, analysis.EvidenceIDs...)
			finding.Metadata["evidence_llm_used"] = true
			finding.Metadata["evidence_fallback_used"] = false
			finding.Metadata["supported_signals"] = analysis.SupportedSignals
			finding.Metadata["suspected_failure_pattern"] =
				analysis.SuspectedFailurePattern
			finding.Metadata["missing_evidence"] = analysis.MissingEvidence
		} else {
			finding.Metadata["evidence_llm_error"] = "analysis_failed"
		}
	}
	return finding, nil
}

func (a *EvidenceAgent) invoke(
	ctx context.Context,
	toolName string,
	arguments any,
	language string,
) (common.ToolResult, agenteino.ToolRun, *agenteino.Limitation) {
	started := time.Now()
	run := agenteino.ToolRun{Tool: toolName}
	current, ok := a.tools[toolName]
	if !ok {
		run.DurationMS = time.Since(started).Milliseconds()
		run.ErrorCode = common.ErrorCodeDependencyUnavailable
		return common.ToolResult{}, run, &agenteino.Limitation{
			Code: "EVIDENCE_TOOL_UNAVAILABLE",
			Tool: toolName,
			Message: localizedTriageText(
				language,
				"工具 "+toolName+" 不可用；已继续分析其他观测证据。",
				"Tool "+toolName+" is unavailable; remaining observability evidence was still analyzed.",
			),
		}
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		run.DurationMS = time.Since(started).Milliseconds()
		run.ErrorCode = common.ErrorCodeInternal
		return common.ToolResult{}, run, evidenceToolLimitation(
			toolName,
			language,
			"EVIDENCE_ARGUMENTS_INVALID",
		)
	}

	agenteino.EmitStreamEvent(
		ctx,
		"tool_call_started",
		map[string]any{"tool": toolName, "agent_role": string(AgentRoleEvidence)},
	)
	raw, err := current.InvokableRun(ctx, string(encoded))
	run.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		run.ErrorCode = common.ErrorCodeInternal
		agenteino.EmitStreamEvent(ctx, "tool_call_failed", map[string]any{
			"tool":       toolName,
			"agent_role": string(AgentRoleEvidence),
			"error_code": string(run.ErrorCode),
			"latency_ms": run.DurationMS,
		})
		return common.ToolResult{}, run, evidenceToolLimitation(
			toolName,
			language,
			"EVIDENCE_TOOL_FAILED",
		)
	}
	var result common.ToolResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		run.ErrorCode = common.ErrorCodeInternal
		return common.ToolResult{}, run, evidenceToolLimitation(
			toolName,
			language,
			"EVIDENCE_RESULT_INVALID",
		)
	}
	run.Success = result.Success && result.Error == nil
	run.DurationMS = result.DurationMS
	run.EvidenceCount = len(result.Evidence)
	run.WarningCount = len(result.Warnings)
	if result.Error != nil {
		run.ErrorCode = result.Error.Code
		return common.ToolResult{}, run, evidenceToolLimitation(
			toolName,
			language,
			string(result.Error.Code),
		)
	}
	agenteino.EmitStreamEvent(ctx, "tool_call_completed", map[string]any{
		"tool":           toolName,
		"agent_role":     string(AgentRoleEvidence),
		"evidence_count": len(result.Evidence),
		"warning_count":  len(result.Warnings),
		"latency_ms":     run.DurationMS,
	})
	return result, run, nil
}

func evidenceArguments(source string, plan TriagePlan) any {
	switch source {
	case "metrics":
		return metrics.Input{
			Service:    plan.Service,
			MetricName: metricIntent(plan.IncidentType),
			Symptom:    plan.IncidentType,
			TimeRange:  plan.TimeContext,
		}
	case "logs":
		return logs.Input{
			Service:   plan.Service,
			TimeRange: plan.TimeContext,
			Keywords:  logKeywords(plan),
			Level:     "error",
		}
	case "alerts":
		return alerts.Input{
			Service: plan.Service,
			Window:  "30m",
		}
	case "traces":
		return traces.Input{
			Service:   plan.Service,
			TimeRange: plan.TimeContext,
		}
	case "topology":
		return topology.Input{
			Service: plan.Service,
			Depth:   1,
		}
	default:
		return map[string]any{}
	}
}

func metricIntent(incidentType string) string {
	switch incidentType {
	case IncidentPaymentTimeout:
		return "payment_dependency_latency"
	case IncidentLatency:
		return "checkout_p95_latency"
	default:
		return "http_server_error_rate"
	}
}

func logKeywords(plan TriagePlan) []string {
	if plan.Language == "zh" {
		return []string{"error", "timeout", "错误", "超时"}
	}
	return []string{"error", "timeout"}
}

func evidenceToolLimitation(
	toolName string,
	language string,
	code string,
) *agenteino.Limitation {
	return &agenteino.Limitation{
		Code: code,
		Tool: toolName,
		Message: localizedTriageText(
			language,
			"工具 "+toolName+" 未返回可用结果；已继续分析其他证据。",
			"Tool "+toolName+" did not return a usable result; remaining evidence was still analyzed.",
		),
	}
}

func noEvidenceLimitation(
	source string,
	toolName string,
	language string,
) agenteino.Limitation {
	return agenteino.Limitation{
		Code: strings.ToUpper(source) + "_NO_DATA",
		Tool: toolName,
		Message: localizedTriageText(
			language,
			toolName+" 本次未返回证据，不能据此确认根因。",
			toolName+" returned no evidence, so it cannot confirm a root cause.",
		),
	}
}

func observationSummary(
	language string,
	service string,
	summaries []string,
	limitationCount int,
) string {
	if len(summaries) == 0 {
		return localizedTriageText(
			language,
			"未取得 "+service+" 的可用观测证据；请查看 limitations。",
			"No usable observability evidence was returned for "+service+"; review limitations.",
		)
	}
	joined := strings.Join(summaries, "；")
	if language == "zh" {
		return "观测证据摘要（service=" + service + "）：" + joined +
			fmt.Sprintf("。limitations=%d。", limitationCount)
	}
	return "Observability summary (service=" + service + "): " + joined +
		fmt.Sprintf(". limitations=%d.", limitationCount)
}

func boundedSummary(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}
