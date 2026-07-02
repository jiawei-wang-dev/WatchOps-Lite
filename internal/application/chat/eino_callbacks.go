package chat

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
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
			ctx, span := observability.StartSpan(ctx, spanName)
			return context.WithValue(ctx, graphCallbackSpanKey{}, span)
		}).
		OnEndFn(func(
			ctx context.Context,
			_ *callbacks.RunInfo,
			_ callbacks.CallbackOutput,
		) context.Context {
			endChatGraphCallbackSpan(ctx, "")
			return ctx
		}).
		OnErrorFn(func(
			ctx context.Context,
			_ *callbacks.RunInfo,
			_ error,
		) context.Context {
			endChatGraphCallbackSpan(ctx, "native Eino graph node failed")
			return ctx
		}).
		Build()
}

func chatGraphSpanName(info *callbacks.RunInfo) string {
	if info == nil {
		return ""
	}
	if info.Name == graphName {
		return "workflow.chat"
	}
	switch info.Name {
	case nodeLoadSessionContext,
		nodeLoadLongTermMemory,
		nodeBuildPromptInput,
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
