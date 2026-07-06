package chat

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

const (
	graphName                = "watchops_chat"
	nodeNormalizeChatInput   = "normalize_chat_input"
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
			node: compose.InvokableLambda(collectToolEvidenceGraphNode),
		},
		{
			key: nodePersistSessionMemory,
			node: compose.InvokableLambda(
				service.persistSessionMemoryGraphNode,
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
		{nodeNormalizeChatInput, nodeLoadSessionContext},
		{nodeNormalizeChatInput, nodeLoadLongTermMemory},
		{nodeNormalizeChatInput, nodePrepareSkills},
		{nodeNormalizeChatInput, nodeLoadUserProfile},
		{nodeNormalizeChatInput, nodePreRetrieveKnowledge},
		{nodeLoadSessionContext, nodeMergeContext},
		{nodeLoadLongTermMemory, nodeMergeContext},
		{nodePrepareSkills, nodeMergeContext},
		{nodeLoadUserProfile, nodeMergeContext},
		{nodePreRetrieveKnowledge, nodeMergeContext},
		{nodeMergeContext, nodeRenderPromptTemplate},
		{nodeRenderPromptTemplate, nodeRunReActAgent},
		{nodeRunReActAgent, nodeCollectToolEvidence},
		{nodeCollectToolEvidence, nodePersistSessionMemory},
		{nodePersistSessionMemory, nodeBuildChatResponse},
		{nodeBuildChatResponse, compose.END},
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
