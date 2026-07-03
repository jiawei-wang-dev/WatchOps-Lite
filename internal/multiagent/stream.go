package multiagent

import (
	"context"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
)

type StreamEvent struct {
	Type string
	Data map[string]any
}

type StreamEmitter func(StreamEvent)

func (s *Service) Stream(
	ctx context.Context,
	command Command,
	emit StreamEmitter,
) (Result, error) {
	safeEmit := func(eventType string, data map[string]any) {
		if emit == nil || eventType == "" {
			return
		}
		if data == nil {
			data = map[string]any{}
		}
		if _, exists := data["request_id"]; !exists && command.RequestID != "" {
			data["request_id"] = command.RequestID
		}
		if _, exists := data["timestamp"]; !exists {
			data["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		emit(StreamEvent{Type: eventType, Data: data})
	}
	safeEmit("multi_agent_started", map[string]any{
		"orchestrator": "eino_graph",
		"latency_ms":   int64(0),
	})
	if emit != nil {
		ctx = agenteino.ContextWithStreamEmitter(
			ctx,
			func(event agenteino.StreamEvent) {
				data := cloneMultiAgentStreamData(event.Data)
				if _, exists := data["request_id"]; !exists &&
					command.RequestID != "" {
					data["request_id"] = command.RequestID
				}
				if _, exists := data["timestamp"]; !exists {
					data["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
				}
				emit(StreamEvent{Type: event.Type, Data: data})
			},
		)
	}
	result, err := s.Execute(ctx, command)
	if err != nil {
		return Result{}, err
	}
	safeEmit("evidence_collected", map[string]any{
		"trace_id":       result.TraceID,
		"evidence_count": len(result.Output.Evidence),
		"tool_run_count": len(result.Output.ToolRuns),
	})
	return result, nil
}

func cloneMultiAgentStreamData(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
