package chat

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/trace"
)

type graphCallbackSpanKey struct{}

func newChatGraphCallbacks() callbacks.Handler {
	return callbacks.NewHandlerBuilder().
		OnStartFn(func(
			ctx context.Context,
			info *callbacks.RunInfo,
			_ callbacks.CallbackInput,
		) context.Context {
			spanName := chatGraphSpanName(info)
			if spanName == "" {
				return ctx
			}
			emitChatGraphStreamEvent(ctx, info, true)
			ctx, span := observability.StartSpan(ctx, spanName)
			return context.WithValue(ctx, graphCallbackSpanKey{}, span)
		}).
		OnEndFn(func(
			ctx context.Context,
			info *callbacks.RunInfo,
			_ callbacks.CallbackOutput,
		) context.Context {
			emitChatGraphStreamEvent(ctx, info, false)
			endChatGraphCallbackSpan(ctx, "")
			return ctx
		}).
		OnErrorFn(func(
			ctx context.Context,
			info *callbacks.RunInfo,
			_ error,
		) context.Context {
			emitChatGraphStreamEvent(ctx, info, false)
			endChatGraphCallbackSpan(ctx, "native Eino graph node failed")
			return ctx
		}).
		Build()
}

func emitChatGraphStreamEvent(ctx context.Context, info *callbacks.RunInfo, started bool) {
	if info == nil || info.Name == "" || info.Name == graphName {
		return
	}
	eventType := "graph_node_completed"
	if started {
		eventType = "graph_node_started"
	}
	agenteino.EmitStreamEvent(ctx, eventType, map[string]any{
		"node": info.Name,
	})
}

func chatGraphSpanName(info *callbacks.RunInfo) string {
	if info == nil {
		return ""
	}
	if info.Name == graphName {
		return "workflow.chat"
	}
	switch info.Name {
	case nodeNormalizeChatInput,
		nodeLoadSessionContext,
		nodeLoadLongTermMemory,
		nodeLoadUserProfile,
		nodePrepareSkills,
		nodeMergeContext,
		nodeRenderPromptTemplate,
		nodeRunReActAgent,
		nodeCollectToolEvidence,
		nodePersistSessionMemory,
		nodeBuildChatResponse:
		return "graph." + info.Name
	default:
		return ""
	}
}

func endChatGraphCallbackSpan(ctx context.Context, errorMessage string) {
	span, ok := ctx.Value(graphCallbackSpanKey{}).(trace.Span)
	if !ok || span == nil {
		return
	}
	if errorMessage != "" {
		observability.MarkError(span, errorMessage)
	}
	span.End()
}
