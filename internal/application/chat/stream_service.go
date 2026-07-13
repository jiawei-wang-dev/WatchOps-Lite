package chat

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

	safeEmit("workflow_started", map[string]any{
		"latency_ms": int64(0),
	})
	if emit != nil {
		ctx = agenteino.ContextWithStreamEmitter(ctx, func(event agenteino.StreamEvent) {
			data := cloneStreamData(event.Data)
			if _, exists := data["request_id"]; !exists && command.RequestID != "" {
				data["request_id"] = command.RequestID
			}
			emit(StreamEvent{Type: event.Type, Data: data})
		})
	}

	result, err := s.Execute(ctx, command)
	if err != nil {
		return Result{}, err
	}
	traceID := result.TraceID
	if traceID != "" {
		safeEmit("memory_loaded", map[string]any{
			"trace_id":                      traceID,
			"session_memory_available":      result.Agent.Metadata["session_memory_available"],
			"long_term_memory_loaded_count": result.Agent.Metadata["long_term_memory_loaded_count"],
			"long_term_memory_count":        result.Agent.Metadata["long_term_memory_count"],
		})
	}
	safeEmit("evidence_collected", map[string]any{
		"trace_id":       traceID,
		"evidence_count": len(result.Agent.Evidence),
		"tool_run_count": len(result.Agent.ToolRuns),
	})
	if triggered, _ := result.Agent.Metadata["failure_controller_triggered"].(bool); triggered {
		safeEmit("failure_controller_triggered", map[string]any{
			"trace_id":       traceID,
			"failure_reason": result.Agent.Metadata["failure_reason"],
		})
	}
	return result, nil
}

func cloneStreamData(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
