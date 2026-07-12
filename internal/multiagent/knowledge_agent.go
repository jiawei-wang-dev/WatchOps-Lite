package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
)

const defaultLongTermMemoryLimit = 3

type KnowledgeAgent struct {
	tool                einotool.InvokableTool
	longTermMemory      longterm.Store
	longTermMemoryLimit int
	retrievalTimeout    time.Duration
	llm                 *RoleLLM
}

func NewKnowledgeAgent(
	ctx context.Context,
	tools []einotool.InvokableTool,
	longTermMemory longterm.Store,
	longTermMemoryLimit int,
) (*KnowledgeAgent, error) {
	var knowledgeTool einotool.InvokableTool
	for _, current := range tools {
		if current == nil {
			continue
		}
		info, err := current.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read Eino tool info: %w", err)
		}
		if info.Name == knowledge.Name {
			knowledgeTool = current
			break
		}
	}
	if longTermMemoryLimit <= 0 {
		longTermMemoryLimit = defaultLongTermMemoryLimit
	}
	return &KnowledgeAgent{
		tool:                knowledgeTool,
		longTermMemory:      longTermMemory,
		longTermMemoryLimit: longTermMemoryLimit,
		retrievalTimeout:    8 * time.Second,
	}, nil
}

func (a *KnowledgeAgent) WithLLM(llm *RoleLLM) *KnowledgeAgent {
	a.llm = llm
	return a
}

func (a *KnowledgeAgent) WithRetrievalTimeout(timeout time.Duration) *KnowledgeAgent {
	if timeout > 0 {
		a.retrievalTimeout = timeout
	}
	return a
}

func (a *KnowledgeAgent) Analyze(
	ctx context.Context,
	plan TriagePlan,
) (AgentFinding, error) {
	// Runbooks and long-term memory are guidance, not proof of the current
	// incident. The final answer must cite current evidence before turning this
	// guidance into a recommendation.
	finding := AgentFinding{
		Role:        AgentRoleKnowledge,
		Evidence:    []common.EvidenceItem{},
		EvidenceIDs: []string{},
		ToolRuns:    []agenteino.ToolRun{},
		Limitations: []agenteino.Limitation{},
		Metadata: map[string]any{
			"role_skill_cards": plan.AgentPlan.RoleSkillCards[AgentRoleKnowledge],
			"role_skill_names": roleSkillNamesForRole(
				plan.AgentPlan.RoleSkillHints,
				AgentRoleKnowledge,
			),
		},
	}
	for key, value := range roleContextMetadata(plan.Metadata["session_context"], AgentRoleKnowledge) {
		finding.Metadata[key] = value
	}
	summaries := []string{}
	retrievalCtx := ctx
	cancelRetrieval := func() {}
	if a.retrievalTimeout > 0 {
		retrievalCtx, cancelRetrieval = context.WithTimeout(ctx, a.retrievalTimeout)
	}
	defer cancelRetrieval()
	roleRAGChunks := plan.RoleRAG.ChunksByRole[AgentRoleKnowledge]
	if len(roleRAGChunks) > 0 {
		roleEvidence := roleRAGChunksAsEvidence(roleRAGChunks)
		finding.Evidence = append(finding.Evidence, roleEvidence...)
		for _, item := range roleEvidence {
			finding.EvidenceIDs = append(finding.EvidenceIDs, item.ID)
		}
		summaries = append(
			summaries,
			"pre-rag: "+boundedSummary(roleRAGChunks[0].Content, 260),
		)
		finding.Metadata["role_rag_chunk_count"] = len(roleRAGChunks)
	}
	if planIncludesSource(plan, "knowledge") {
		result, run, limitation := a.invokeKnowledge(retrievalCtx, plan)
		finding.ToolRuns = append(finding.ToolRuns, run)
		if limitation != nil {
			finding.Limitations = append(finding.Limitations, *limitation)
		} else if len(result.Evidence) == 0 {
			finding.Limitations = append(finding.Limitations, agenteino.Limitation{
				Code: "KNOWLEDGE_NO_DATA",
				Tool: knowledge.Name,
				Message: localizedTriageText(
					plan.Language,
					"search_knowledge 未命中相关 runbook，建议不能视为已由知识库验证。",
					"search_knowledge found no relevant runbook, so recommendations are not knowledge-validated.",
				),
			})
		} else {
			finding.Evidence = append(finding.Evidence, result.Evidence...)
			for _, item := range result.Evidence {
				finding.EvidenceIDs = append(finding.EvidenceIDs, item.ID)
			}
			summaries = append(
				summaries,
				"runbook: "+boundedSummary(result.Evidence[0].Content, 260),
			)
		}
	} else {
		finding.Metadata["knowledge_search_skipped"] = true
	}

	memories := []longterm.Memory{}
	if a.longTermMemory != nil {
		result, err := a.longTermMemory.Search(retrievalCtx, longterm.SearchQuery{
			Query:   plan.Query,
			Service: plan.Service,
			Limit:   a.longTermMemoryLimit,
		})
		if err != nil {
			finding.Metadata["long_term_memory_available"] = false
			finding.Metadata["long_term_memory_not_configured"] = false
			finding.Metadata["long_term_memory_error"] = "search_failed"
			finding.Limitations = append(finding.Limitations, agenteino.Limitation{
				Code: "LONG_TERM_MEMORY_UNAVAILABLE",
				Message: localizedTriageText(
					plan.Language,
					"MySQL 长期记忆不可用；本次知识分析未使用跨会话经验。",
					"MySQL long-term memory is unavailable; no cross-session experience was used.",
				),
			})
		} else {
			finding.Metadata["long_term_memory_available"] = true
			finding.Metadata["long_term_memory_not_configured"] = false
			memories = result
			for _, memory := range memories {
				summary := strings.TrimSpace(memory.Summary)
				if summary == "" {
					summary = strings.TrimSpace(memory.Title)
				}
				if summary != "" {
					summaries = append(
						summaries,
						"memory: "+boundedSummary(summary, 180),
					)
				}
			}
		}
	} else {
		finding.Metadata["long_term_memory_available"] = false
		finding.Metadata["long_term_memory_not_configured"] = true
	}
	finding.Metadata["long_term_memory_count"] = len(memories)
	if len(finding.Evidence) > 0 || len(memories) > 0 {
		normalizedEvidence, idMap := stableKnowledgeEvidence(finding.Evidence, memories)
		finding.Evidence = normalizedEvidence
		finding.EvidenceIDs = evidenceIDsForPrompt(normalizedEvidence)
		finding.Metadata["allowed_evidence_ids"] = append([]string{}, finding.EvidenceIDs...)
		finding.Metadata["evidence_id_map"] = idMap
	}
	finding.Metadata["knowledge_evidence_count"] = len(finding.Evidence)
	finding.Summary = knowledgeSummary(
		plan.Language,
		plan.Service,
		summaries,
		len(finding.Limitations),
	)
	finding.Metadata["knowledge_llm_used"] = false
	finding.Metadata["knowledge_llm_attempted"] = false
	finding.Metadata["knowledge_model"] = ""
	finding.Metadata["knowledge_fallback_used"] = true
	finding.Metadata["knowledge_llm_duration_ms"] = int64(0)
	for key, value := range roleLLMNotConfiguredMetadata(
		AgentRoleKnowledge,
		"knowledge_llm_not_configured",
	) {
		finding.Metadata[key] = value
	}
	if a.llm != nil {
		finding.Metadata["knowledge_llm_attempted"] = true
		analysis, call, err := a.llm.analyzeKnowledge(
			ctx,
			plan,
			finding.Evidence,
			memories,
			finding.Limitations,
		)
		finding.Metadata["knowledge_model"] = a.llm.modelName
		finding.Metadata["knowledge_llm_duration_ms"] = call.durationMS
		if err == nil {
			for key, value := range roleLLMMetadata(roleLLMMetadataInput{
				Role:         AgentRoleKnowledge,
				Model:        a.llm.modelName,
				Attempted:    true,
				Success:      true,
				Call:         call,
				Fallback:     false,
				AnalysisMode: "llm",
			}) {
				finding.Metadata[key] = value
			}
			finding.Summary = strings.TrimSpace(analysis.KnowledgeSummary)
			finding.EvidenceIDs = append([]string{}, analysis.EvidenceIDs...)
			finding.Metadata["knowledge_llm_used"] = true
			finding.Metadata["knowledge_fallback_used"] = false
			finding.Metadata["runbook_supported_actions"] = []string(analysis.RunbookActions)
			finding.Metadata["historical_patterns"] = analysis.HistoricalPatterns
			finding.Metadata["unsafe_actions_to_avoid"] =
				analysis.UnsafeActionsToAvoid
		} else {
			for key, value := range roleLLMMetadata(roleLLMMetadataInput{
				Role:           AgentRoleKnowledge,
				Model:          a.llm.modelName,
				Attempted:      true,
				Success:        false,
				Call:           call,
				Fallback:       true,
				FallbackReason: "knowledge_llm_analysis_failed",
				AnalysisMode:   "fallback",
			}) {
				finding.Metadata[key] = value
			}
			finding.Metadata["knowledge_llm_error"] = call.errorCode
		}
	}
	return finding, nil
}

func (a *KnowledgeAgent) invokeKnowledge(
	ctx context.Context,
	plan TriagePlan,
) (common.ToolResult, agenteino.ToolRun, *agenteino.Limitation) {
	started := time.Now()
	run := agenteino.ToolRun{
		Tool:            knowledge.Name,
		ExecutionStatus: "running",
		DataStatus:      "unknown",
		Metadata:        map[string]any{},
	}
	if a.tool == nil {
		run.DurationMS = time.Since(started).Milliseconds()
		run.ErrorCode = common.ErrorCodeDependencyUnavailable
		run.ErrorMessage = "tool is unavailable"
		run.ExecutionStatus = "failed"
		return common.ToolResult{}, run, &agenteino.Limitation{
			Code: "KNOWLEDGE_TOOL_UNAVAILABLE",
			Tool: knowledge.Name,
			Message: localizedTriageText(
				plan.Language,
				"search_knowledge 不可用；本次综合结论不能引用 runbook。",
				"search_knowledge is unavailable; synthesis cannot cite a runbook.",
			),
		}
	}
	encoded, err := json.Marshal(knowledge.Input{
		Query:    plan.Query,
		TopK:     3,
		Category: "runbook",
	})
	if err != nil {
		run.ErrorCode = common.ErrorCodeInternal
		run.ErrorMessage = "tool arguments could not be encoded"
		run.ExecutionStatus = "failed"
		return common.ToolResult{}, run, knowledgeToolLimitation(
			plan.Language,
			"KNOWLEDGE_ARGUMENTS_INVALID",
		)
	}
	agenteino.EmitStreamEvent(ctx, "tool_call_started", map[string]any{
		"tool":       knowledge.Name,
		"agent_role": string(AgentRoleKnowledge),
	})
	raw, err := a.tool.InvokableRun(ctx, string(encoded))
	run.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		run.ErrorCode = common.ErrorCodeInternal
		run.ErrorMessage = err.Error()
		run.ExecutionStatus = "failed"
		agenteino.EmitStreamEvent(ctx, "tool_call_failed", map[string]any{
			"tool":       knowledge.Name,
			"agent_role": string(AgentRoleKnowledge),
			"error_code": string(run.ErrorCode),
			"latency_ms": run.DurationMS,
		})
		return common.ToolResult{}, run, knowledgeToolLimitation(
			plan.Language,
			"KNOWLEDGE_TOOL_FAILED",
		)
	}
	var result common.ToolResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		run.ErrorCode = common.ErrorCodeInternal
		run.ErrorMessage = "tool returned invalid JSON"
		run.ExecutionStatus = "failed"
		return common.ToolResult{}, run, knowledgeToolLimitation(
			plan.Language,
			"KNOWLEDGE_RESULT_INVALID",
		)
	}
	run.Success = result.Success && result.Error == nil
	run.DurationMS = result.DurationMS
	run.EvidenceCount = len(result.Evidence)
	run.WarningCount = len(result.Warnings)
	run.EvidenceIDs = collectMultiAgentEvidenceIDs(result.Evidence)
	run.Metadata = result.Metadata
	run.FallbackUsed = agenteino.ToolResultFallbackUsed(result)
	run.DataStatus = agenteino.ToolResultDataStatus(result)
	run.ExecutionStatus = "success"
	if result.Error != nil {
		run.ErrorCode = result.Error.Code
		run.ErrorMessage = result.Error.Message
		run.ExecutionStatus = "failed"
		return common.ToolResult{}, run, knowledgeToolLimitation(
			plan.Language,
			string(result.Error.Code),
		)
	}
	agenteino.EmitStreamEvent(ctx, "tool_call_completed", map[string]any{
		"tool":             knowledge.Name,
		"agent_role":       string(AgentRoleKnowledge),
		"evidence_count":   len(result.Evidence),
		"evidence_ids":     run.EvidenceIDs,
		"warning_count":    len(result.Warnings),
		"latency_ms":       run.DurationMS,
		"execution_status": run.ExecutionStatus,
		"data_status":      run.DataStatus,
		"fallback_used":    run.FallbackUsed,
	})
	return result, run, nil
}

func knowledgeToolLimitation(
	language string,
	code string,
) *agenteino.Limitation {
	return &agenteino.Limitation{
		Code: code,
		Tool: knowledge.Name,
		Message: localizedTriageText(
			language,
			"search_knowledge 未返回可用结果；本次知识分析已安全降级。",
			"search_knowledge returned no usable result; knowledge analysis degraded safely.",
		),
	}
}

func planIncludesSource(plan TriagePlan, source string) bool {
	for _, current := range plan.EvidencePlan {
		if current == source {
			return true
		}
	}
	return false
}

func knowledgeSummary(
	language string,
	service string,
	summaries []string,
	limitationCount int,
) string {
	if len(summaries) == 0 {
		return localizedTriageText(
			language,
			"未取得 "+service+" 的可用 runbook 或历史经验；请查看 limitations。",
			"No usable runbook or historical experience was returned for "+service+"; review limitations.",
		)
	}
	joined := strings.Join(summaries, "；")
	if language == "zh" {
		return "知识分析摘要（service=" + service + "）：" + joined +
			fmt.Sprintf("。limitations=%d。", limitationCount)
	}
	return "Knowledge summary (service=" + service + "): " + joined +
		fmt.Sprintf(". limitations=%d.", limitationCount)
}

func stableKnowledgeEvidence(
	evidence []common.EvidenceItem,
	memories []longterm.Memory,
) ([]common.EvidenceItem, map[string]string) {
	result := make([]common.EvidenceItem, 0, len(evidence)+len(memories))
	idMap := map[string]string{}
	nextID := func() string {
		return fmt.Sprintf("knowledge_%d", len(result)+1)
	}
	for _, item := range evidence {
		stableID := nextID()
		originalID := strings.TrimSpace(item.ID)
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["source_internal_id"] = originalID
		item.Metadata["stable_evidence_id"] = stableID
		item.ID = stableID
		result = append(result, item)
		idMap[stableID] = originalID
	}
	for _, memory := range memories {
		summary := strings.TrimSpace(memory.Summary)
		if summary == "" {
			summary = strings.TrimSpace(memory.Title)
		}
		if summary == "" {
			continue
		}
		stableID := nextID()
		result = append(result, common.EvidenceItem{
			ID:         stableID,
			SourceType: "memory",
			SourceName: "long-term-memory",
			Content:    summary,
			ResourceID: memory.Service,
			Metadata: map[string]any{
				"source_internal_id":       memory.ID,
				"stable_evidence_id":       stableID,
				"title":                    memory.Title,
				"service":                  memory.Service,
				"evidence_origin":          "historical_memory",
				"evidence_weight":          "contextual",
				"can_confirm_current_fact": false,
				"can_support_hypothesis":   true,
				"source_type":              memory.SourceType,
			},
		})
		idMap[stableID] = memory.ID
	}
	return result, idMap
}
