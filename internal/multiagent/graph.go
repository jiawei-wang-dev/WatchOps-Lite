package multiagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

const (
	graphName          = "watchops_multi_agent"
	nodeNormalizeInput = "normalize_multi_agent_input"
	nodeTriage         = "triage_agent"
	nodeEvidence       = "evidence_agent"
	nodeKnowledge      = "knowledge_agent"
	nodeMergeFindings  = "merge_agent_findings"
	nodeSynthesis      = "synthesis_agent"
	nodeBuildResponse  = "build_multi_agent_response"
)

var ErrExecution = errors.New("multi-agent execution failed")

type Input struct {
	RequestID   string
	SessionID   string
	UserID      string
	Message     string
	TimeContext common.TimeRange
	Metadata    map[string]any
}

type TriagePlanner interface {
	Plan(ctx context.Context, input Input) (TriagePlan, error)
}

type FindingAnalyzer interface {
	Analyze(ctx context.Context, plan TriagePlan) (AgentFinding, error)
}

type SynthesisInput struct {
	Plan             TriagePlan
	EvidenceFinding  AgentFinding
	KnowledgeFinding AgentFinding
	Evidence         []common.EvidenceItem
	ToolRuns         []agenteino.ToolRun
	Limitations      []agenteino.Limitation
}

type Synthesizer interface {
	Synthesize(ctx context.Context, input SynthesisInput) (agenteino.AgentOutput, error)
}

type graphRunner interface {
	Invoke(
		ctx context.Context,
		input Input,
		opts ...compose.Option,
	) (MultiAgentResult, error)
}

type Orchestrator struct {
	triage    TriagePlanner
	evidence  FindingAnalyzer
	knowledge FindingAnalyzer
	synthesis Synthesizer
	graph     graphRunner
	graphErr  error
	now       func() time.Time
}

type normalizedInput struct {
	Input Input
}

type triageOutput struct {
	Input Input
	Plan  TriagePlan
	Step  AgentStep
}

type findingOutput struct {
	Triage  triageOutput
	Finding AgentFinding
	Step    AgentStep
}

type mergedOutput struct {
	Triage triageOutput
	Merged MergedFindings
	Steps  []AgentStep
}

type synthesisOutput struct {
	Merged mergedOutput
	Answer agenteino.AgentOutput
	Step   AgentStep
}

func NewOrchestrator(
	ctx context.Context,
	triage TriagePlanner,
	evidence FindingAnalyzer,
	knowledge FindingAnalyzer,
	synthesis Synthesizer,
) *Orchestrator {
	orchestrator := &Orchestrator{
		triage:    triage,
		evidence:  evidence,
		knowledge: knowledge,
		synthesis: synthesis,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
	orchestrator.graph, orchestrator.graphErr = compileGraph(ctx, orchestrator)
	return orchestrator
}

func (o *Orchestrator) Execute(
	ctx context.Context,
	input Input,
) (MultiAgentResult, error) {
	if o.graphErr != nil || o.graph == nil {
		return MultiAgentResult{}, fmt.Errorf(
			"%w: Eino multi-agent graph is unavailable",
			ErrExecution,
		)
	}
	result, err := o.graph.Invoke(ctx, input)
	if err != nil {
		return MultiAgentResult{}, fmt.Errorf("%w: %v", ErrExecution, err)
	}
	return result, nil
}

func compileGraph(
	ctx context.Context,
	orchestrator *Orchestrator,
) (compose.Runnable[Input, MultiAgentResult], error) {
	if orchestrator.triage == nil ||
		orchestrator.evidence == nil ||
		orchestrator.knowledge == nil ||
		orchestrator.synthesis == nil {
		return nil, errors.New("all multi-agent roles are required")
	}

	graph := compose.NewGraph[Input, MultiAgentResult]()
	nodes := []struct {
		key     string
		node    *compose.Lambda
		options []compose.GraphAddNodeOpt
	}{
		{
			key:  nodeNormalizeInput,
			node: compose.InvokableLambda(orchestrator.normalizeInput),
		},
		{
			key:  nodeTriage,
			node: compose.InvokableLambda(orchestrator.runTriage),
		},
		{
			key:  nodeEvidence,
			node: compose.InvokableLambda(orchestrator.runEvidence),
			options: []compose.GraphAddNodeOpt{
				compose.WithOutputKey(nodeEvidence),
			},
		},
		{
			key:  nodeKnowledge,
			node: compose.InvokableLambda(orchestrator.runKnowledge),
			options: []compose.GraphAddNodeOpt{
				compose.WithOutputKey(nodeKnowledge),
			},
		},
		{
			key:  nodeMergeFindings,
			node: compose.InvokableLambda(orchestrator.mergeFindings),
		},
		{
			key:  nodeSynthesis,
			node: compose.InvokableLambda(orchestrator.runSynthesis),
		},
		{
			key:  nodeBuildResponse,
			node: compose.InvokableLambda(orchestrator.buildResponse),
		},
	}
	for _, current := range nodes {
		options := append(
			[]compose.GraphAddNodeOpt{compose.WithNodeName(current.key)},
			current.options...,
		)
		if err := graph.AddLambdaNode(current.key, current.node, options...); err != nil {
			return nil, fmt.Errorf(
				"add Eino multi-agent graph node %q: %w",
				current.key,
				err,
			)
		}
	}

	edges := [][2]string{
		{compose.START, nodeNormalizeInput},
		{nodeNormalizeInput, nodeTriage},
		{nodeTriage, nodeEvidence},
		{nodeTriage, nodeKnowledge},
		{nodeEvidence, nodeMergeFindings},
		{nodeKnowledge, nodeMergeFindings},
		{nodeMergeFindings, nodeSynthesis},
		{nodeSynthesis, nodeBuildResponse},
		{nodeBuildResponse, compose.END},
	}
	for _, edge := range edges {
		if err := graph.AddEdge(edge[0], edge[1]); err != nil {
			return nil, fmt.Errorf(
				"add Eino multi-agent graph edge %q -> %q: %w",
				edge[0],
				edge[1],
				err,
			)
		}
	}

	runnable, err := graph.Compile(
		ctx,
		compose.WithGraphName(graphName),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
	if err != nil {
		return nil, fmt.Errorf("compile Eino multi-agent graph: %w", err)
	}
	return runnable, nil
}

func (o *Orchestrator) normalizeInput(
	ctx context.Context,
	input Input,
) (normalizedInput, error) {
	ctx, span := observability.StartSpan(ctx, "multiagent.normalize_input")
	defer span.End()
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.UserID = strings.TrimSpace(input.UserID)
	input.Message = strings.TrimSpace(input.Message)
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if input.SessionID == "" || input.Message == "" {
		observability.MarkError(span, "multi-agent input is incomplete")
		return normalizedInput{}, errors.New("session_id and message are required")
	}
	if err := input.TimeContext.Validate(); err != nil {
		observability.MarkError(span, "multi-agent time context is invalid")
		return normalizedInput{}, err
	}
	return normalizedInput{Input: input}, nil
}

func (o *Orchestrator) runTriage(
	ctx context.Context,
	input normalizedInput,
) (triageOutput, error) {
	started := o.now()
	ctx, span := observability.StartSpan(ctx, "multiagent.triage")
	defer span.End()
	emitAgentStepEvent(ctx, "agent_step_started", AgentRoleTriage, "", 0, 0)
	plan, err := o.triage.Plan(ctx, input.Input)
	if err != nil {
		observability.MarkError(span, "Triage Agent failed")
		return triageOutput{}, err
	}
	span.SetAttributes(
		attribute.String("agent.role", string(AgentRoleTriage)),
		attribute.String("agent.mode", metadataString(plan.Metadata, "triage_mode")),
		attribute.String("llm.model", metadataString(plan.Metadata, "triage_model")),
		attribute.Bool("llm.used", metadataBool(plan.Metadata, "triage_llm_used")),
		attribute.Bool("fallback.used", metadataBool(plan.Metadata, "triage_fallback_used")),
		attribute.String("incident_type", plan.IncidentType),
		attribute.String("service", plan.Service),
		attribute.Int("evidence_plan_count", len(plan.EvidencePlan)),
	)
	completed := o.now()
	output := plan.Summary
	if output == "" {
		output = plan.Query
	}
	emitAgentStepEvent(
		ctx,
		"agent_step_completed",
		AgentRoleTriage,
		string(AgentStepCompleted),
		completed.Sub(started).Milliseconds(),
		0,
	)
	step := completedStep(
		AgentRoleTriage,
		"Triage Agent",
		input.Input.Message,
		output,
		nil,
		nil,
		plan.Limitations,
		started,
		completed,
	)
	step.Metadata = cloneMetadata(plan.Metadata)
	return triageOutput{
		Input: input.Input,
		Plan:  plan,
		Step:  step,
	}, nil
}

func (o *Orchestrator) runEvidence(
	ctx context.Context,
	input triageOutput,
) (findingOutput, error) {
	return o.runFinding(
		ctx,
		"multiagent.evidence",
		AgentRoleEvidence,
		"Evidence Agent",
		input,
		o.evidence,
	)
}

func (o *Orchestrator) runKnowledge(
	ctx context.Context,
	input triageOutput,
) (findingOutput, error) {
	return o.runFinding(
		ctx,
		"multiagent.knowledge",
		AgentRoleKnowledge,
		"Knowledge Agent",
		input,
		o.knowledge,
	)
}

func (o *Orchestrator) runFinding(
	ctx context.Context,
	spanName string,
	role AgentRole,
	name string,
	input triageOutput,
	analyzer FindingAnalyzer,
) (findingOutput, error) {
	started := o.now()
	ctx, span := observability.StartSpan(
		ctx,
		spanName,
		attribute.String("agent.role", string(role)),
	)
	defer span.End()
	emitAgentStepEvent(ctx, "agent_step_started", role, "", 0, 0)
	finding, err := analyzer.Analyze(ctx, input.Plan)
	if err != nil {
		observability.MarkError(span, name+" failed")
		completed := o.now()
		limitation := agenteino.Limitation{
			Code: strings.ToUpper(string(role)) + "_AGENT_FAILED",
			Message: localizedTriageText(
				input.Plan.Language,
				name+" 执行失败；综合诊断将仅使用其余可用证据。",
				name+" failed; synthesis will use only the remaining available evidence.",
			),
		}
		finding := AgentFinding{
			Role:        role,
			Summary:     limitation.Message,
			Evidence:    []common.EvidenceItem{},
			EvidenceIDs: []string{},
			ToolRuns:    []agenteino.ToolRun{},
			Limitations: []agenteino.Limitation{limitation},
			Metadata:    map[string]any{"agent_failed": true},
		}
		emitAgentStepEvent(
			ctx,
			"agent_step_completed",
			role,
			string(AgentStepFailed),
			completed.Sub(started).Milliseconds(),
			0,
		)
		return findingOutput{
			Triage:  input,
			Finding: finding,
			Step: failedStep(
				role,
				name,
				input.Plan.Query,
				limitation.Message,
				limitation,
				started,
				completed,
			),
		}, nil
	}
	finding.Role = role
	completed := o.now()
	emitAgentStepEvent(
		ctx,
		"agent_step_completed",
		role,
		string(AgentStepCompleted),
		completed.Sub(started).Milliseconds(),
		len(finding.Evidence),
	)
	step := completedStep(
		role,
		name,
		input.Plan.Query,
		finding.Summary,
		finding.EvidenceIDs,
		finding.ToolRuns,
		finding.Limitations,
		started,
		completed,
	)
	step.Metadata = cloneMetadata(finding.Metadata)
	return findingOutput{
		Triage:  input,
		Finding: finding,
		Step:    step,
	}, nil
}

func (o *Orchestrator) mergeFindings(
	ctx context.Context,
	input map[string]any,
) (mergedOutput, error) {
	ctx, span := observability.StartSpan(ctx, "multiagent.merge_findings")
	defer span.End()
	emitAgentStepEvent(ctx, "agent_step_started", "merge", "", 0, 0)
	evidence, evidenceOK := input[nodeEvidence].(findingOutput)
	knowledge, knowledgeOK := input[nodeKnowledge].(findingOutput)
	if !evidenceOK || !knowledgeOK {
		observability.MarkError(span, "multi-agent findings are incomplete")
		return mergedOutput{}, errors.New("multi-agent findings are incomplete")
	}
	merged := MergeAgentFindings(
		evidence.Triage.Plan,
		evidence.Finding,
		knowledge.Finding,
	)
	emitAgentStepEvent(
		ctx,
		"agent_step_completed",
		"merge",
		string(AgentStepCompleted),
		0,
		len(merged.Evidence),
	)
	return mergedOutput{
		Triage: evidence.Triage,
		Merged: merged,
		Steps: []AgentStep{
			evidence.Triage.Step,
			evidence.Step,
			knowledge.Step,
		},
	}, nil
}

func (o *Orchestrator) runSynthesis(
	ctx context.Context,
	input mergedOutput,
) (synthesisOutput, error) {
	started := o.now()
	ctx, span := observability.StartSpan(ctx, "multiagent.synthesis")
	defer span.End()
	agenteino.EmitStreamEvent(ctx, "synthesis_started", map[string]any{
		"agent_role": string(AgentRoleSynthesis),
	})
	emitAgentStepEvent(ctx, "agent_step_started", AgentRoleSynthesis, "", 0, 0)
	answer, err := o.synthesis.Synthesize(ctx, SynthesisInput{
		Plan:             input.Merged.Plan,
		EvidenceFinding:  input.Merged.EvidenceFinding,
		KnowledgeFinding: input.Merged.KnowledgeFinding,
		Evidence:         input.Merged.Evidence,
		ToolRuns:         input.Merged.ToolRuns,
		Limitations:      input.Merged.Limitations,
	})
	if err != nil {
		observability.MarkError(span, "Synthesis Agent failed")
		return synthesisOutput{}, err
	}
	completed := o.now()
	evidenceIDs := make([]string, 0, len(answer.Evidence))
	for _, item := range answer.Evidence {
		evidenceIDs = append(evidenceIDs, item.ID)
	}
	emitAgentStepEvent(
		ctx,
		"agent_step_completed",
		AgentRoleSynthesis,
		string(AgentStepCompleted),
		completed.Sub(started).Milliseconds(),
		len(answer.Evidence),
	)
	step := completedStep(
		AgentRoleSynthesis,
		"Synthesis Agent",
		input.Triage.Plan.Query,
		firstConclusion(answer),
		evidenceIDs,
		nil,
		answer.Limitations,
		started,
		completed,
	)
	step.Metadata = cloneMetadata(answer.Metadata)
	return synthesisOutput{
		Merged: input,
		Answer: answer,
		Step:   step,
	}, nil
}

func (o *Orchestrator) buildResponse(
	ctx context.Context,
	input synthesisOutput,
) (MultiAgentResult, error) {
	_, span := observability.StartSpan(ctx, "multiagent.build_response")
	defer span.End()
	steps := append([]AgentStep{}, input.Merged.Steps...)
	steps = append(steps, input.Step)
	fallbackUsed, _ := input.Answer.Metadata["fallback_used"].(bool)
	synthesisMode, _ := input.Answer.Metadata["synthesis_mode"].(string)
	triageMetadata := input.Merged.Triage.Plan.Metadata
	evidenceMetadata := input.Merged.Merged.EvidenceFinding.Metadata
	knowledgeMetadata := input.Merged.Merged.KnowledgeFinding.Metadata
	synthesisMetadata := input.Answer.Metadata
	triageUsed := metadataBool(triageMetadata, "triage_llm_used")
	evidenceUsed := metadataBool(evidenceMetadata, "evidence_llm_used")
	knowledgeUsed := metadataBool(knowledgeMetadata, "knowledge_llm_used")
	synthesisUsed := metadataBool(synthesisMetadata, "synthesis_llm_used")
	llmCallCount := 0
	for _, role := range []struct {
		metadata map[string]any
		key      string
	}{
		{triageMetadata, "triage_llm_attempted"},
		{evidenceMetadata, "evidence_llm_attempted"},
		{knowledgeMetadata, "knowledge_llm_attempted"},
		{synthesisMetadata, "synthesis_llm_attempted"},
	} {
		if metadataBool(role.metadata, role.key) {
			llmCallCount++
		}
	}
	llmRoles := make([]string, 0, 4)
	if triageUsed {
		llmRoles = append(llmRoles, string(AgentRoleTriage))
	}
	if evidenceUsed {
		llmRoles = append(llmRoles, string(AgentRoleEvidence))
	}
	if knowledgeUsed {
		llmRoles = append(llmRoles, string(AgentRoleKnowledge))
	}
	if synthesisUsed {
		llmRoles = append(llmRoles, string(AgentRoleSynthesis))
	}
	metadata := map[string]any{
		"agent_mode":                 "multi_agent",
		"orchestrator":               "eino_graph",
		"roles":                      RoleOrder(),
		"fallback_used":              fallbackUsed,
		"synthesis_mode":             synthesisMode,
		"multi_agent_llm_used":       len(llmRoles) > 0,
		"multi_agent_llm_roles":      llmRoles,
		"multi_agent_llm_call_count": llmCallCount,
	}
	copyRoleLLMMetadata(metadata, triageMetadata, "triage")
	copyRoleLLMMetadata(metadata, evidenceMetadata, "evidence")
	copyRoleLLMMetadata(metadata, knowledgeMetadata, "knowledge")
	copyRoleLLMMetadata(metadata, synthesisMetadata, "synthesis")
	ensureRoleLLMMetadata(metadata, "triage")
	ensureRoleLLMMetadata(metadata, "evidence")
	ensureRoleLLMMetadata(metadata, "knowledge")
	ensureRoleLLMMetadata(metadata, "synthesis")
	return MultiAgentResult{
		Steps:       steps,
		Evidence:    input.Merged.Merged.Evidence,
		ToolRuns:    input.Merged.Merged.ToolRuns,
		FinalAnswer: input.Answer,
		Metadata:    metadata,
	}, nil
}

func ensureRoleLLMMetadata(metadata map[string]any, role string) {
	defaults := map[string]any{
		role + "_llm_used":        false,
		role + "_llm_attempted":   false,
		role + "_model":           "",
		role + "_fallback_used":   true,
		role + "_llm_duration_ms": int64(0),
		role + "_mode":            "rule_based",
	}
	for key, value := range defaults {
		if _, exists := metadata[key]; !exists {
			metadata[key] = value
		}
	}
}

func copyRoleLLMMetadata(
	target map[string]any,
	source map[string]any,
	role string,
) {
	for _, suffix := range []string{
		"llm_used",
		"llm_attempted",
		"model",
		"fallback_used",
		"llm_duration_ms",
		"mode",
	} {
		key := role + "_" + suffix
		if value, exists := source[key]; exists {
			target[key] = value
		}
	}
}

func metadataBool(metadata map[string]any, key string) bool {
	value, _ := metadata[key].(bool)
	return value
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return value
}

func cloneMetadata(metadata map[string]any) map[string]any {
	result := make(map[string]any, len(metadata))
	for key, value := range metadata {
		result[key] = value
	}
	return result
}

func completedStep(
	role AgentRole,
	name string,
	input string,
	output string,
	evidenceIDs []string,
	toolRuns []agenteino.ToolRun,
	limitations []agenteino.Limitation,
	started time.Time,
	completed time.Time,
) AgentStep {
	return AgentStep{
		Role:        role,
		Name:        name,
		Status:      AgentStepCompleted,
		Input:       input,
		Output:      output,
		EvidenceIDs: append([]string{}, evidenceIDs...),
		ToolRuns:    append([]agenteino.ToolRun{}, toolRuns...),
		Limitations: append([]agenteino.Limitation{}, limitations...),
		StartedAt:   started,
		CompletedAt: completed,
		DurationMS:  completed.Sub(started).Milliseconds(),
	}
}

func failedStep(
	role AgentRole,
	name string,
	input string,
	output string,
	limitation agenteino.Limitation,
	started time.Time,
	completed time.Time,
) AgentStep {
	return AgentStep{
		Role:        role,
		Name:        name,
		Status:      AgentStepFailed,
		Input:       input,
		Output:      output,
		EvidenceIDs: []string{},
		ToolRuns:    []agenteino.ToolRun{},
		Limitations: []agenteino.Limitation{limitation},
		StartedAt:   started,
		CompletedAt: completed,
		DurationMS:  completed.Sub(started).Milliseconds(),
		Metadata:    map[string]any{"agent_failed": true},
	}
}

func firstConclusion(output agenteino.AgentOutput) string {
	if len(output.Conclusions) == 0 {
		return ""
	}
	return output.Conclusions[0].Text
}

func emitAgentStepEvent(
	ctx context.Context,
	eventType string,
	role AgentRole,
	status string,
	durationMS int64,
	evidenceCount int,
) {
	data := map[string]any{
		"agent_role": string(role),
	}
	if status != "" {
		data["status"] = status
	}
	if durationMS > 0 {
		data["duration_ms"] = durationMS
	}
	if evidenceCount > 0 {
		data["evidence_count"] = evidenceCount
	}
	agenteino.EmitStreamEvent(ctx, eventType, data)
}
