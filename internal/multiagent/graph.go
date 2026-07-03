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
	Triage           triageOutput
	EvidenceFinding  AgentFinding
	KnowledgeFinding AgentFinding
	Evidence         []common.EvidenceItem
	ToolRuns         []agenteino.ToolRun
	Limitations      []agenteino.Limitation
	Steps            []AgentStep
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
	plan, err := o.triage.Plan(ctx, input.Input)
	if err != nil {
		observability.MarkError(span, "Triage Agent failed")
		return triageOutput{}, err
	}
	completed := o.now()
	output := plan.Summary
	if output == "" {
		output = plan.Query
	}
	return triageOutput{
		Input: input.Input,
		Plan:  plan,
		Step: completedStep(
			AgentRoleTriage,
			"Triage Agent",
			input.Input.Message,
			output,
			nil,
			nil,
			plan.Limitations,
			started,
			completed,
		),
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
	finding, err := analyzer.Analyze(ctx, input.Plan)
	if err != nil {
		observability.MarkError(span, name+" failed")
		return findingOutput{}, err
	}
	finding.Role = role
	completed := o.now()
	return findingOutput{
		Triage:  input,
		Finding: finding,
		Step: completedStep(
			role,
			name,
			input.Plan.Query,
			finding.Summary,
			finding.EvidenceIDs,
			finding.ToolRuns,
			finding.Limitations,
			started,
			completed,
		),
	}, nil
}

func (o *Orchestrator) mergeFindings(
	ctx context.Context,
	input map[string]any,
) (mergedOutput, error) {
	ctx, span := observability.StartSpan(ctx, "multiagent.merge_findings")
	defer span.End()
	evidence, evidenceOK := input[nodeEvidence].(findingOutput)
	knowledge, knowledgeOK := input[nodeKnowledge].(findingOutput)
	if !evidenceOK || !knowledgeOK {
		observability.MarkError(span, "multi-agent findings are incomplete")
		return mergedOutput{}, errors.New("multi-agent findings are incomplete")
	}
	return mergedOutput{
		Triage:           evidence.Triage,
		EvidenceFinding:  evidence.Finding,
		KnowledgeFinding: knowledge.Finding,
		Evidence: append(
			append([]common.EvidenceItem{}, evidence.Finding.Evidence...),
			knowledge.Finding.Evidence...,
		),
		ToolRuns: append(
			append([]agenteino.ToolRun{}, evidence.Finding.ToolRuns...),
			knowledge.Finding.ToolRuns...,
		),
		Limitations: append(
			append(
				[]agenteino.Limitation{},
				evidence.Triage.Plan.Limitations...,
			),
			append(
				evidence.Finding.Limitations,
				knowledge.Finding.Limitations...,
			)...,
		),
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
	answer, err := o.synthesis.Synthesize(ctx, SynthesisInput{
		Plan:             input.Triage.Plan,
		EvidenceFinding:  input.EvidenceFinding,
		KnowledgeFinding: input.KnowledgeFinding,
		Evidence:         input.Evidence,
		ToolRuns:         input.ToolRuns,
		Limitations:      input.Limitations,
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
	return synthesisOutput{
		Merged: input,
		Answer: answer,
		Step: completedStep(
			AgentRoleSynthesis,
			"Synthesis Agent",
			input.Triage.Plan.Query,
			firstConclusion(answer),
			evidenceIDs,
			nil,
			answer.Limitations,
			started,
			completed,
		),
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
	return MultiAgentResult{
		Steps:       steps,
		Evidence:    input.Merged.Evidence,
		ToolRuns:    input.Merged.ToolRuns,
		FinalAnswer: input.Answer,
		Metadata: map[string]any{
			"agent_mode":   "multi_agent",
			"orchestrator": "eino_graph",
			"roles":        RoleOrder(),
		},
	}, nil
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

func firstConclusion(output agenteino.AgentOutput) string {
	if len(output.Conclusions) == 0 {
		return ""
	}
	return output.Conclusions[0].Text
}
