package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/skills"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/profile"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge/queryplan"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

type graphState struct {
	command                   Command
	snapshot                  session.ContextSnapshot
	memoryAvailable           bool
	longTermMemories          []longterm.Memory
	longTermMemoryUnavailable bool
	agentInput                agenteino.AgentInput
	renderedMessages          []*schema.Message
	promptRenderFailed        bool
	agentOutput               agenteino.AgentOutput
	intentResult              intent.IntentResult
}

type normalizedChatInput struct {
	command Command
}

type intentBranch struct {
	command Command
	result  intent.IntentResult
}

type sessionContextBranch struct {
	command         Command
	snapshot        session.ContextSnapshot
	memoryAvailable bool
}

type longTermMemoryBranch struct {
	memories    []longterm.Memory
	unavailable bool
}

type diagnosticSkillsBranch struct {
	cards    []string
	metadata map[string]any
}

type userProfileBranch struct {
	contextLines []string
}

type preRAGBranch struct {
	chunks      []retrievalknowledge.RetrievedKnowledge
	metadata    map[string]any
	limitations []string
	available   bool
}

func normalizeChatInputGraphNode(
	_ context.Context,
	command Command,
) (normalizedChatInput, error) {
	command.SessionID = strings.TrimSpace(command.SessionID)
	command.UserID = strings.TrimSpace(command.UserID)
	command.Message = strings.TrimSpace(command.Message)
	return normalizedChatInput{command: command}, nil
}

func (s *Service) recognizeIntentGraphNode(
	ctx context.Context,
	input normalizedChatInput,
) (intentBranch, error) {
	if s.intentRecognizer == nil {
		return intentBranch{
			command: input.command,
			result: intent.SafeDefault(input.command.Message, intent.IntentLimitation{
				Code:    "INTENT_RECOGNIZER_UNAVAILABLE",
				Message: "Intent recognizer is unavailable; safe default intent was used.",
			}),
		}, nil
	}
	result, err := s.intentRecognizer.Recognize(ctx, intent.RecognitionInput{
		Message:        input.command.Message,
		SessionID:      input.command.SessionID,
		UserID:         input.command.UserID,
		Now:            s.now(),
		AvailableTools: []string{"query_metrics", "query_logs", "query_traces", "search_knowledge"},
		AvailableSkills: []string{
			"metric_inspection",
			"log_investigation",
			"trace_inspection",
			"runbook_lookup",
			"checkout_incident_diagnosis",
		},
	})
	if err != nil {
		result = intent.SafeDefault(input.command.Message, intent.IntentLimitation{
			Code:    "INTENT_RECOGNITION_FAILED",
			Message: "Intent recognition failed; safe default intent was used.",
		})
	}
	result = intent.Normalize(result)
	agenteino.EmitStreamEvent(ctx, "intent_recognized", map[string]any{
		"intent":           result.Intent,
		"confidence":       result.Confidence,
		"source":           result.Source,
		"suggested_tools":  result.SuggestedTools,
		"suggested_agents": result.SuggestedAgents,
		"fallback_used":    result.Metadata["fallback_used"],
	})
	return intentBranch{command: input.command, result: result}, nil
}

func (s *Service) loadUserProfileGraphNode(
	ctx context.Context,
	input map[string]any,
) (userProfileBranch, error) {
	branch, err := intentBranchFromInput(input)
	if err != nil {
		return userProfileBranch{}, err
	}
	if branch.command.UserID == "" || s.profileLoader == nil {
		return userProfileBranch{contextLines: []string{}}, nil
	}
	value, err := s.profileLoader.LoadProfile(ctx, branch.command.UserID)
	if err != nil {
		return userProfileBranch{contextLines: []string{}}, nil
	}
	return userProfileBranch{contextLines: profile.ContextLines(value)}, nil
}

func (s *Service) loadSessionContextGraphNode(
	ctx context.Context,
	input map[string]any,
) (sessionContextBranch, error) {
	branch, err := intentBranchFromInput(input)
	if err != nil {
		return sessionContextBranch{}, err
	}
	snapshot, available := s.loadContext(ctx, branch.command.SessionID)
	return sessionContextBranch{
		command:         branch.command,
		snapshot:        snapshot,
		memoryAvailable: available,
	}, nil
}

func (s *Service) loadLongTermMemoryGraphNode(
	ctx context.Context,
	input map[string]any,
) (longTermMemoryBranch, error) {
	branch, err := intentBranchFromInput(input)
	if err != nil {
		return longTermMemoryBranch{}, err
	}
	if s.longTermMemory == nil {
		return longTermMemoryBranch{memories: []longterm.Memory{}}, nil
	}
	memories, err := s.longTermMemory.Search(ctx, longterm.SearchQuery{
		Query: branch.command.Message,
		Limit: s.longTermMemoryTopK,
	})
	if err != nil {
		return longTermMemoryBranch{
			memories:    []longterm.Memory{},
			unavailable: true,
		}, nil
	}
	return longTermMemoryBranch{memories: memories}, nil
}

func prepareDiagnosticSkillsGraphNode(
	ctx context.Context,
	input map[string]any,
) (diagnosticSkillsBranch, error) {
	branch, err := intentBranchFromInput(input)
	if err != nil {
		return diagnosticSkillsBranch{}, err
	}
	ctx, span := observability.StartSpan(
		ctx,
		"intent.skills.select",
		attribute.String("intent.type", string(branch.result.Intent)),
	)
	defer span.End()
	all := diagnosticSkillDefinitions()
	selected := intent.SelectSkillsForIntent(all, branch.result)
	cards := formatDiagnosticSkillCards(selected)
	metadata := map[string]any{
		"selected_skill_count": len(cards),
		"filtered_by_intent":   len(cards) != len(all),
		"intent_type":          string(branch.result.Intent),
	}
	span.SetAttributes(
		attribute.Int("selected_skills_count", len(cards)),
		attribute.Bool("filtered_by_intent", len(cards) != len(all)),
	)
	return diagnosticSkillsBranch{cards: cards, metadata: metadata}, nil
}

func (s *Service) preRetrieveKnowledgeGraphNode(
	ctx context.Context,
	input map[string]any,
) (preRAGBranch, error) {
	branch, err := intentBranchFromInput(input)
	if err != nil {
		return preRAGBranch{}, err
	}
	agenteino.EmitStreamEvent(ctx, "pre_rag_started", map[string]any{
		"query_length":       len(branch.command.Message),
		"intent_type":        string(branch.result.Intent),
		"query_plan_enabled": true,
	})
	ctx, span := observability.StartSpan(
		ctx,
		"chat.pre_rag",
		attribute.Int("query_length", len(branch.command.Message)),
		attribute.Int("top_k", s.preRAGTopK),
		attribute.String("intent.type", string(branch.result.Intent)),
	)
	defer span.End()
	if !intent.ShouldRunPreRAG(branch.result) {
		metadata := map[string]any{
			"pre_rag_used":        false,
			"reason":              "skipped_by_intent",
			"pre_rag_intent_type": string(branch.result.Intent),
			"rag_hints_applied":   false,
		}
		span.SetAttributes(attribute.Bool("pre_rag_used", false))
		agenteino.EmitStreamEvent(ctx, "pre_rag_completed", map[string]any{
			"chunk_count": 0,
			"intent_type": string(branch.result.Intent),
			"metadata":    metadata,
		})
		return preRAGBranch{metadata: metadata}, nil
	}
	if s.knowledgeRetriever == nil {
		span.SetAttributes(attribute.Bool("pre_rag_used", false))
		agenteino.EmitStreamEvent(ctx, "pre_rag_failed", map[string]any{
			"error_code": "PRE_RAG_UNAVAILABLE",
		})
		return preRAGBranch{
			metadata: map[string]any{
				"pre_rag_used": false,
				"reason":       "knowledge_retriever_unavailable",
			},
			limitations: []string{"PRE_RAG_UNAVAILABLE"},
		}, nil
	}
	ragContext, ragSpan := observability.StartSpan(
		ctx,
		"intent.rag.apply",
		attribute.String("intent.type", string(branch.result.Intent)),
		attribute.Int("top_k", preRAGTopK(branch.result, s.preRAGTopK)),
	)
	result, err := s.multiQueryPreRAG(ragContext, branch)
	ragSpan.SetAttributes(attribute.Bool("rag_hints_applied", true))
	ragSpan.End()
	if err != nil {
		observability.MarkError(span, "chat pre-rag failed")
		span.SetAttributes(attribute.Bool("pre_rag_used", false))
		agenteino.EmitStreamEvent(ctx, "pre_rag_failed", map[string]any{
			"error_code": "PRE_RAG_UNAVAILABLE",
		})
		return preRAGBranch{
			metadata: map[string]any{
				"pre_rag_used": false,
				"reason":       "hybrid_retrieve_failed",
			},
			limitations: []string{"PRE_RAG_UNAVAILABLE"},
		}, nil
	}
	metadata := cloneAnyMap(result.Metadata)
	metadata["pre_rag_used"] = len(result.Chunks) > 0
	metadata["pre_rag_chunk_count"] = len(result.Chunks)
	metadata["pre_rag_intent_type"] = string(branch.result.Intent)
	metadata["rag_hints_applied"] = true
	span.SetAttributes(
		attribute.Bool("pre_rag_used", len(result.Chunks) > 0),
		attribute.Int("pre_rag_chunk_count", len(result.Chunks)),
	)
	agenteino.EmitStreamEvent(ctx, "pre_rag_completed", map[string]any{
		"chunk_count":          len(result.Chunks),
		"intent_type":          string(branch.result.Intent),
		"sub_query_count":      metadata["rag_sub_query_count"],
		"selected_chunk_count": metadata["selected_chunk_count"],
		"metadata":             metadata,
	})
	return preRAGBranch{
		chunks:    result.Chunks,
		metadata:  metadata,
		available: true,
	}, nil
}

func (s *Service) multiQueryPreRAG(
	ctx context.Context,
	branch intentBranch,
) (retrievalknowledge.RetrievalResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"rag.multi_query_retrieve",
		attribute.String("intent.type", string(branch.result.Intent)),
	)
	defer span.End()

	plan, err := s.planPreRAGQueries(ctx, branch)
	if err != nil {
		return s.singleQueryPreRAG(ctx, branch, true)
	}
	results := make([]queryplan.RAGSubQueryResult, 0, len(plan.Queries))
	topK := preRAGTopK(branch.result, s.preRAGTopK)
	for _, subQuery := range plan.Queries {
		request := intent.BuildRetrievalRequest(subQuery.Query, branch.result, s.preRAGTopK)
		request.Query = subQuery.Query
		result, retrieveErr := s.knowledgeRetriever.HybridRetrieve(ctx, request)
		results = append(results, queryplan.RAGSubQueryResult{
			Query:  subQuery,
			Result: result,
			Error:  retrieveErr,
		})
	}
	_, mergeSpan := observability.StartSpan(
		ctx,
		"rag.multi_query_merge",
		attribute.Int("rag.sub_query_count", len(plan.Queries)),
	)
	merged := queryplan.MergeResults(plan, results, topK)
	mergeSpan.SetAttributes(attribute.Int("selected_chunk_count", len(merged.Chunks)))
	mergeSpan.End()
	if len(merged.Chunks) == 0 {
		return s.singleQueryPreRAG(ctx, branch, true)
	}
	span.SetAttributes(
		attribute.Int("rag.sub_query_count", len(plan.Queries)),
		attribute.Int("selected_chunk_count", len(merged.Chunks)),
		attribute.Bool("query_rewrite_applied", len(plan.Queries) > 1),
	)
	return merged, nil
}

func preRAGTopK(result intent.IntentResult, fallback int) int {
	return intent.BuildRetrievalRequest("", result, fallback).TopK
}

func (s *Service) planPreRAGQueries(
	ctx context.Context,
	branch intentBranch,
) (queryplan.RAGQueryPlan, error) {
	if s.ragQueryPlanner == nil {
		return queryplan.RAGQueryPlan{}, fmt.Errorf("%w: rag query planner unavailable", ErrExecution)
	}
	return s.ragQueryPlanner.Plan(ctx, queryplan.QueryPlanInput{
		UserMessage: branch.command.Message,
		Intent:      branch.result,
		Service:     branch.result.Service,
		Symptom:     branch.result.Symptom,
		Keywords:    branch.result.Keywords,
		Now:         s.now(),
	})
}

func (s *Service) singleQueryPreRAG(
	ctx context.Context,
	branch intentBranch,
	plannerFallback bool,
) (retrievalknowledge.RetrievalResult, error) {
	request := intent.BuildRetrievalRequest(branch.command.Message, branch.result, s.preRAGTopK)
	result, err := s.knowledgeRetriever.HybridRetrieve(ctx, request)
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["rag_query_plan_source"] = "fallback_single_query"
	result.Metadata["rag_sub_query_count"] = 1
	result.Metadata["rag_sub_query_types"] = []string{string(queryplan.QueryOriginal)}
	result.Metadata["query_rewrite_applied"] = false
	result.Metadata["query_plan_fallback_used"] = plannerFallback
	return result, err
}

func mergeContextGraphNode(
	_ context.Context,
	input map[string]any,
) (graphState, error) {
	sessionBranch, ok := input[nodeLoadSessionContext].(sessionContextBranch)
	if !ok {
		return graphState{}, fmt.Errorf("%w: session context branch output is unavailable", ErrExecution)
	}
	memoryBranch, ok := input[nodeLoadLongTermMemory].(longTermMemoryBranch)
	if !ok {
		return graphState{}, fmt.Errorf("%w: long-term memory branch output is unavailable", ErrExecution)
	}
	skillsBranch, ok := input[nodePrepareSkills].(diagnosticSkillsBranch)
	if !ok {
		return graphState{}, fmt.Errorf("%w: diagnostic skills branch output is unavailable", ErrExecution)
	}
	profileBranch, ok := input[nodeLoadUserProfile].(userProfileBranch)
	if !ok {
		return graphState{}, fmt.Errorf("%w: user profile branch output is unavailable", ErrExecution)
	}
	preRAGBranch, ok := input[nodePreRetrieveKnowledge].(preRAGBranch)
	if !ok {
		return graphState{}, fmt.Errorf("%w: pre-rag branch output is unavailable", ErrExecution)
	}
	intentBranch, ok := input[nodeRecognizeIntent].(intentBranch)
	if !ok {
		return graphState{}, fmt.Errorf("%w: intent branch output is unavailable", ErrExecution)
	}
	return graphState{
		command:                   sessionBranch.command,
		snapshot:                  sessionBranch.snapshot,
		memoryAvailable:           sessionBranch.memoryAvailable,
		longTermMemories:          memoryBranch.memories,
		longTermMemoryUnavailable: memoryBranch.unavailable,
		intentResult:              intentBranch.result,
		agentInput: agenteino.AgentInput{
			SessionSummary: sessionBranch.snapshot.Summary,
			RecentMessages: sessionBranch.snapshot.RecentMessages,
			ConfirmedLongTermMemories: confirmedLongTermMemoryPrompt(
				memoryBranch.memories,
			),
			DiagnosticSkills:   skillsBranch.cards,
			Intent:             intentBranch.result,
			UserProfileContext: profileBranch.contextLines,
			RetrievedKnowledge: preRetrievedKnowledgePrompt(preRAGBranch.chunks),
			PreRetrievedKnowledge: append(
				[]retrievalknowledge.RetrievedKnowledge{},
				preRAGBranch.chunks...,
			),
			PreRAGAvailable:   preRAGBranch.available,
			PreRAGMetadata:    preRAGBranch.metadata,
			PreRAGLimitations: append([]string{}, preRAGBranch.limitations...),
			CurrentMessage:    sessionBranch.command.Message,
			TimeContext:       sessionBranch.command.TimeContext,
		},
	}, nil
}

func intentBranchFromInput(input map[string]any) (intentBranch, error) {
	branch, ok := input[nodeRecognizeIntent].(intentBranch)
	if !ok {
		return intentBranch{}, fmt.Errorf("%w: intent branch output is unavailable", ErrExecution)
	}
	return branch, nil
}

func (s *Service) renderPromptTemplateGraphNode(
	ctx context.Context,
	state graphState,
) (graphState, error) {
	renderer, ok := s.runner.(agenteino.PromptRenderingRunner)
	if !ok {
		return state, nil
	}
	messages, err := renderer.RenderPrompt(ctx, state.agentInput)
	if err != nil {
		// Run the normal runner in the next node so its existing deterministic
		// fallback policy remains authoritative for prompt-render failures.
		state.promptRenderFailed = true
		return state, nil
	}
	state.renderedMessages = messages
	return state, nil
}

func (s *Service) runReActAgentGraphNode(
	ctx context.Context,
	state graphState,
) (graphState, error) {
	var (
		output agenteino.AgentOutput
		err    error
	)
	prepared, supportsPrepared := s.runner.(agenteino.PromptRenderingRunner)
	if supportsPrepared && !state.promptRenderFailed {
		output, err = prepared.RunPrepared(
			ctx,
			state.agentInput,
			state.renderedMessages,
		)
	} else {
		output, err = s.runner.Run(ctx, state.agentInput)
	}
	if err != nil {
		return graphState{}, fmt.Errorf("%w: %v", ErrExecution, err)
	}
	state.agentOutput = output
	return state, nil
}

func (s *Service) collectToolEvidenceGraphNode(
	ctx context.Context,
	state graphState,
) (graphState, error) {
	// Eino and deterministic runners already normalize evidence through the
	// unified Evidence model. This node marks the graph boundary and attaches
	// only application-level memory availability metadata.
	ensureAgentMetadata(&state.agentOutput)
	s.processAgentEvidence(ctx, &state.agentOutput)
	state.agentOutput.Metadata["session_memory_available"] = state.memoryAvailable
	state.agentOutput.Metadata["long_term_memory_count"] = len(state.longTermMemories)
	state.agentOutput.Metadata["pre_rag_used"] = state.agentInput.PreRAGAvailable
	state.agentOutput.Metadata["pre_rag_chunk_count"] = len(state.agentInput.PreRetrievedKnowledge)
	state.agentOutput.Metadata["pre_rag"] = state.agentInput.PreRAGMetadata
	state.agentOutput.Metadata["intent"] = state.intentResult
	state.agentOutput.Metadata["intent_type"] = string(state.intentResult.Intent)
	state.agentOutput.Metadata["intent_source"] = state.intentResult.Source
	for _, code := range state.agentInput.PreRAGLimitations {
		state.agentOutput.Limitations = append(
			state.agentOutput.Limitations,
			agenteino.Limitation{
				Code:    code,
				Message: "Pre-RAG knowledge retrieval was unavailable; the answer may rely on tools and other context only.",
				Tool:    "search_knowledge",
			},
		)
	}
	if state.longTermMemoryUnavailable {
		state.agentOutput.Limitations = append(
			state.agentOutput.Limitations,
			agenteino.Limitation{
				Code:    "LONG_TERM_MEMORY_UNAVAILABLE",
				Message: "Confirmed long-term memory is unavailable; this response was generated without cross-session incident context.",
			},
		)
	}
	return state, nil
}

func (s *Service) processAgentEvidence(
	ctx context.Context,
	output *agenteino.AgentOutput,
) {
	if s.evidenceProcessor == nil {
		return
	}
	items := make([]evidence.Item, 0, len(output.Evidence))
	for _, item := range output.Evidence {
		items = append(items, common.ToEvidenceItem(item))
	}
	report := s.evidenceProcessor.Process(ctx, items)
	citationByID := make(map[string]string, len(report.Items))
	for _, item := range report.Items {
		citationByID[item.ID] = item.CitationID
	}
	for index := range output.Evidence {
		citationID := citationByID[output.Evidence[index].ID]
		if citationID == "" {
			continue
		}
		if output.Evidence[index].Metadata == nil {
			output.Evidence[index].Metadata = map[string]any{}
		}
		output.Evidence[index].Metadata["citation_id"] = citationID
	}
	output.Metadata["processed_evidence"] = report.Items
	output.Metadata["processed_evidence_groups"] = report.Groups
	output.Metadata["evidence_processor"] = report.Metadata
	output.Metadata["evidence_original_count"] = report.Metadata["evidence_original_count"]
	output.Metadata["evidence_deduped_count"] = report.Metadata["evidence_deduped_count"]
	output.Metadata["evidence_group_count"] = report.Metadata["evidence_group_count"]
	output.Metadata["citation_enabled"] = report.Metadata["citation_enabled"]
}

func (s *Service) persistSessionMemoryGraphNode(
	ctx context.Context,
	state graphState,
) (graphState, error) {
	if !state.memoryAvailable {
		appendMemoryLimitation(&state.agentOutput)
		return state, nil
	}
	if err := s.persistContext(
		ctx,
		state.command,
		state.snapshot,
		state.agentOutput,
	); err != nil {
		runtimemetrics.IncSessionMemoryUnavailable()
		state.memoryAvailable = false
		state.agentOutput.Metadata["session_memory_available"] = false
		appendMemoryLimitation(&state.agentOutput)
	}
	return state, nil
}

func buildChatResponseGraphNode(
	ctx context.Context,
	state graphState,
) (Result, error) {
	traceID := observability.TraceID(ctx)
	if traceID != "" {
		state.agentOutput.Metadata["trace_id"] = traceID
	}
	return Result{
		RequestID: state.command.RequestID,
		SessionID: state.command.SessionID,
		Agent:     state.agentOutput,
		TraceID:   traceID,
	}, nil
}

func diagnosticSkillCards() []string {
	return formatDiagnosticSkillCards(diagnosticSkillDefinitions())
}

func diagnosticSkillDefinitions() []intent.SkillCard {
	definitions := []skills.Skill{
		skills.MetricInspectionSkill(),
		skills.LogInvestigationSkill(),
		skills.TraceInspectionSkill(),
		skills.RunbookLookupSkill(),
		skills.CheckoutIncidentDiagnosisSkill(),
	}
	cards := make([]intent.SkillCard, 0, len(definitions))
	for _, definition := range definitions {
		cards = append(cards, intent.SkillCard{
			Name:        definition.Name(),
			Description: definition.Description(),
			ToolNames:   definition.ToolNames(),
		})
	}
	return cards
}

func formatDiagnosticSkillCards(definitions []intent.SkillCard) []string {
	cards := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		cards = append(cards, fmt.Sprintf(
			"%s: %s",
			definition.Name,
			definition.Description,
		))
	}
	return cards
}

func confirmedLongTermMemoryPrompt(memories []longterm.Memory) []string {
	result := make([]string, 0, len(memories))
	for _, memory := range memories {
		summary := truncatePromptText(memory.Summary, 500)
		if summary == "" {
			continue
		}
		value := memory.Title + ": " + summary
		if memory.Service != "" {
			value += " [service=" + memory.Service + "]"
		}
		if len(memory.EvidenceIDs) > 0 {
			value += " [evidence_ids=" + strings.Join(memory.EvidenceIDs, ",") + "]"
		}
		result = append(result, value)
	}
	return result
}

func preRetrievedKnowledgePrompt(
	chunks []retrievalknowledge.RetrievedKnowledge,
) []string {
	result := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		content := truncatePromptText(chunk.Content, 700)
		if content == "" {
			continue
		}
		value := chunk.Title + ": " + content
		if chunk.ChunkID != "" {
			value += " [chunk_id=" + chunk.ChunkID + "]"
		}
		if chunk.RetrievalMethod != "" {
			value += " [retrieval=" + chunk.RetrievalMethod + "]"
		}
		result = append(result, value)
	}
	return result
}

func cloneAnyMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func truncatePromptText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
