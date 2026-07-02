package chat

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

const (
	graphName                = "watchops_chat"
	nodeLoadSessionContext   = "load_session_context"
	nodeLoadLongTermMemory   = "load_long_term_memory"
	nodeBuildPromptInput     = "build_prompt_input"
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
			key: nodeLoadSessionContext,
			node: compose.InvokableLambda(
				service.loadSessionContextGraphNode,
			),
		},
		{
			key:  nodeLoadLongTermMemory,
			node: compose.InvokableLambda(loadLongTermMemoryGraphNode),
		},
		{
			key: nodeBuildPromptInput,
			node: compose.InvokableLambda(
				buildPromptInputGraphNode,
			),
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
		if err := graph.AddLambdaNode(
			current.key,
			current.node,
			compose.WithNodeName(current.key),
		); err != nil {
			return nil, fmt.Errorf("add Eino Chat graph node %q: %w", current.key, err)
		}
	}

	edges := [][2]string{
		{compose.START, nodeLoadSessionContext},
		{nodeLoadSessionContext, nodeLoadLongTermMemory},
		{nodeLoadLongTermMemory, nodeBuildPromptInput},
		{nodeBuildPromptInput, nodeRenderPromptTemplate},
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

	runnable, err := graph.Compile(ctx, compose.WithGraphName(graphName))
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
