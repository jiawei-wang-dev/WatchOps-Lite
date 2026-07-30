package chat

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

const (
	graphName                = "watchops_chat"
	nodeNormalizeChatInput   = "normalize_chat_input"
	nodeLoadSessionFocus     = "load_session_focus"
	nodeRecognizeIntent      = "recognize_intent"
	nodeValidateSlots        = "validate_slots"
	nodeProceedIntent        = "proceed_after_slot_validation"
	nodeBuildClarification   = "build_clarification_response"
	nodePersistClarification = "persist_clarification_state"
	nodeLoadSessionContext   = "load_session_context"
	nodeLoadLongTermMemory   = "load_long_term_memory"
	nodeLoadUserProfile      = "load_user_profile"
	nodePrepareSkills        = "prepare_diagnostic_skills"
	nodePreRetrieveKnowledge = "pre_retrieve_knowledge"
	nodeMergeContext         = "merge_context"
	nodeRenderPromptTemplate = "render_prompt_template"
	nodeRunReActAgent        = "run_react_agent"
	nodeCollectToolEvidence  = "collect_tool_evidence"
	nodePersistSessionMemory = "persist_session_memory"
	nodePersistSessionFocus  = "persist_session_focus"
	nodeBuildChatResponse    = "build_chat_response"
)

type chatGraphRunner interface {
	Invoke(
		ctx context.Context,
		input Command,
		opts ...compose.Option,
	) (Result, error)
}

func compileChatGraph(
	ctx context.Context,
	service *Service,
) (compose.Runnable[Command, Result], error) {
	graph := compose.NewGraph[Command, Result]()
	nodes := []struct {
		key  string
		node *compose.Lambda
	}{
		{
			key:  nodeNormalizeChatInput,
			node: compose.InvokableLambda(normalizeChatInputGraphNode),
		},
		{
			key:  nodeLoadSessionFocus,
			node: compose.InvokableLambda(service.loadSessionFocusGraphNode),
		},
		{
			key:  nodeRecognizeIntent,
			node: compose.InvokableLambda(service.recognizeIntentGraphNode),
		},
		{
			key:  nodeValidateSlots,
			node: compose.InvokableLambda(service.validateSlotsGraphNode),
		},
		{
			key:  nodeProceedIntent,
			node: compose.InvokableLambda(proceedIntentGraphNode),
		},
		{
			key:  nodeBuildClarification,
			node: compose.InvokableLambda(buildClarificationGraphNode),
		},
		{
			key:  nodePersistClarification,
			node: compose.InvokableLambda(service.persistClarificationGraphNode),
		},
		{
			key: nodeLoadSessionContext,
			node: compose.InvokableLambda(
				service.loadSessionContextGraphNode,
			),
		},
		{
			key:  nodeLoadLongTermMemory,
			node: compose.InvokableLambda(service.loadLongTermMemoryGraphNode),
		},
		{
			key:  nodePrepareSkills,
			node: compose.InvokableLambda(prepareDiagnosticSkillsGraphNode),
		},
		{
			key:  nodeLoadUserProfile,
			node: compose.InvokableLambda(service.loadUserProfileGraphNode),
		},
		{
			key:  nodePreRetrieveKnowledge,
			node: compose.InvokableLambda(service.preRetrieveKnowledgeGraphNode),
		},
		{
			key:  nodeMergeContext,
			node: compose.InvokableLambda(mergeContextGraphNode),
		},
		{
			key: nodeRenderPromptTemplate,
			node: compose.InvokableLambda(
				service.renderPromptTemplateGraphNode,
			),
		},
		{
			key: nodeRunReActAgent,
			node: compose.InvokableLambda(
				service.runReActAgentGraphNode,
			),
		},
		{
			key:  nodeCollectToolEvidence,
			node: compose.InvokableLambda(service.collectToolEvidenceGraphNode),
		},
		{
			key: nodePersistSessionMemory,
			node: compose.InvokableLambda(
				service.persistSessionMemoryGraphNode,
			),
		},
		{
			key: nodePersistSessionFocus,
			node: compose.InvokableLambda(
				service.persistSessionFocusGraphNode,
			),
		},
		{
			key:  nodeBuildChatResponse,
			node: compose.InvokableLambda(buildChatResponseGraphNode),
		},
	}
	for _, current := range nodes {
		options := []compose.GraphAddNodeOpt{compose.WithNodeName(current.key)}
		switch current.key {
		case nodeProceedIntent:
			options = append(options, compose.WithOutputKey(nodeProceedIntent))
		case nodeLoadSessionContext:
			options = append(options, compose.WithOutputKey(nodeLoadSessionContext))
		case nodeLoadLongTermMemory:
			options = append(options, compose.WithOutputKey(nodeLoadLongTermMemory))
		case nodePrepareSkills:
			options = append(options, compose.WithOutputKey(nodePrepareSkills))
		case nodeLoadUserProfile:
			options = append(options, compose.WithOutputKey(nodeLoadUserProfile))
		case nodePreRetrieveKnowledge:
			options = append(options, compose.WithOutputKey(nodePreRetrieveKnowledge))
		}
		if err := graph.AddLambdaNode(current.key, current.node, options...); err != nil {
			return nil, fmt.Errorf("add Eino Chat graph node %q: %w", current.key, err)
		}
	}

	edges := [][2]string{
		{compose.START, nodeNormalizeChatInput},
		{nodeNormalizeChatInput, nodeLoadSessionFocus},
		{nodeLoadSessionFocus, nodeRecognizeIntent},
		{nodeRecognizeIntent, nodeValidateSlots},
		{nodeProceedIntent, nodeLoadSessionContext},
		{nodeProceedIntent, nodeLoadLongTermMemory},
		{nodeProceedIntent, nodePrepareSkills},
		{nodeProceedIntent, nodeLoadUserProfile},
		{nodeProceedIntent, nodePreRetrieveKnowledge},
		{nodeProceedIntent, nodeMergeContext},
		{nodeLoadSessionContext, nodeMergeContext},
		{nodeLoadLongTermMemory, nodeMergeContext},
		{nodePrepareSkills, nodeMergeContext},
		{nodeLoadUserProfile, nodeMergeContext},
		{nodePreRetrieveKnowledge, nodeMergeContext},
		{nodeMergeContext, nodeRenderPromptTemplate},
		{nodeRenderPromptTemplate, nodeRunReActAgent},
		{nodeRunReActAgent, nodeCollectToolEvidence},
		{nodeCollectToolEvidence, nodePersistSessionMemory},
		{nodePersistSessionMemory, nodePersistSessionFocus},
		{nodePersistSessionFocus, nodeBuildChatResponse},
		{nodeBuildChatResponse, compose.END},
		{nodeBuildClarification, nodePersistClarification},
		{nodePersistClarification, compose.END},
	}
	for _, edge := range edges {
		if err := graph.AddEdge(edge[0], edge[1]); err != nil {
			return nil, fmt.Errorf(
				"add Eino Chat graph edge %q -> %q: %w",
				edge[0],
				edge[1],
				err,
			)
		}
	}
	if err := graph.AddBranch(
		nodeValidateSlots,
		compose.NewGraphBranch(
			func(_ context.Context, branch decisionBranch) (string, error) {
				if branch.decision.Decision == intent.DecisionClarify {
					return nodeBuildClarification, nil
				}
				return nodeProceedIntent, nil
			},
			map[string]bool{
				nodeProceedIntent:      true,
				nodeBuildClarification: true,
			},
		),
	); err != nil {
		return nil, fmt.Errorf("add slot validation branch: %w", err)
	}

	runnable, err := graph.Compile(
		ctx,
		compose.WithGraphName(graphName),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
	if err != nil {
		return nil, fmt.Errorf("compile native Eino Chat graph: %w", err)
	}
	return runnable, nil
}

func (s *Service) executeGraph(ctx context.Context, command Command) (Result, error) {
	if s.graphErr != nil || s.graph == nil {
		return Result{}, fmt.Errorf("%w: native Eino Chat graph is unavailable", ErrExecution)
	}
	result, err := s.graph.Invoke(
		ctx,
		command,
		compose.WithCallbacks(newChatGraphCallbacks()),
	)
	if err != nil {
		return Result{}, err
	}
	return result, nil
}
