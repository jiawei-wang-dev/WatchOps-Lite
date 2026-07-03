package multiagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

type Command struct {
	RequestID   string
	SessionID   string
	UserID      string
	Message     string
	TimeContext common.TimeRange
	Metadata    map[string]any
}

type Result struct {
	RequestID string
	SessionID string
	Output    MultiAgentResult
	TraceID   string
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

type Service struct {
	orchestrator *Orchestrator
}

func NewService(orchestrator *Orchestrator) *Service {
	return &Service{orchestrator: orchestrator}
}

func (s *Service) Execute(ctx context.Context, command Command) (Result, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"multiagent.execute",
		attribute.String("request_id", strings.TrimSpace(command.RequestID)),
		attribute.String("session_id", strings.TrimSpace(command.SessionID)),
		attribute.Int("message_length", len(command.Message)),
	)
	defer span.End()

	command.RequestID = strings.TrimSpace(command.RequestID)
	command.SessionID = strings.TrimSpace(command.SessionID)
	command.UserID = strings.TrimSpace(command.UserID)
	command.Message = strings.TrimSpace(command.Message)
	if command.SessionID == "" {
		return Result{}, &ValidationError{
			Field:   "session_id",
			Message: "session_id is required",
		}
	}
	if command.Message == "" {
		return Result{}, &ValidationError{
			Field:   "message",
			Message: "message is required",
		}
	}
	if len(command.UserID) > 128 {
		return Result{}, &ValidationError{
			Field:   "user_id",
			Message: "user_id exceeds 128 characters",
		}
	}
	if err := command.TimeContext.Validate(); err != nil {
		return Result{}, &ValidationError{
			Field:   "time_context",
			Message: err.Error(),
		}
	}
	if s.orchestrator == nil {
		observability.MarkError(span, "multi-agent orchestrator unavailable")
		return Result{}, fmt.Errorf("%w: orchestrator unavailable", ErrExecution)
	}

	output, err := s.orchestrator.Execute(ctx, Input{
		RequestID:   command.RequestID,
		SessionID:   command.SessionID,
		UserID:      command.UserID,
		Message:     command.Message,
		TimeContext: command.TimeContext,
		Metadata:    command.Metadata,
	})
	if err != nil {
		observability.MarkError(span, "multi-agent workflow failed")
		return Result{}, err
	}
	traceID := observability.TraceID(ctx)
	if output.Metadata == nil {
		output.Metadata = map[string]any{}
	}
	if traceID != "" {
		output.Metadata["trace_id"] = traceID
	}
	return Result{
		RequestID: command.RequestID,
		SessionID: command.SessionID,
		Output:    output,
		TraceID:   traceID,
	}, nil
}
