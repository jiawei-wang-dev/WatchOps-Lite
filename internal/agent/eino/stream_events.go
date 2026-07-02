package eino

import (
	"context"
	"time"
)

type StreamEvent struct {
	Type string
	Data map[string]any
}

type StreamEmitter func(StreamEvent)

type streamEmitterKey struct{}

func ContextWithStreamEmitter(ctx context.Context, emitter StreamEmitter) context.Context {
	if emitter == nil {
		return ctx
	}
	return context.WithValue(ctx, streamEmitterKey{}, emitter)
}

func EmitStreamEvent(ctx context.Context, eventType string, data map[string]any) {
	emitter, ok := ctx.Value(streamEmitterKey{}).(StreamEmitter)
	if !ok || emitter == nil || eventType == "" {
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	if _, exists := data["timestamp"]; !exists {
		data["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	emitter(StreamEvent{Type: eventType, Data: data})
}
