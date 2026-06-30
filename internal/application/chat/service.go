package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

var ErrExecution = errors.New("chat execution failed")

type Command struct {
	RequestID   string
	SessionID   string
	Message     string
	TimeContext common.TimeRange
}

type Result struct {
	RequestID string
	SessionID string
	Agent     agenteino.AgentOutput
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
	runner agenteino.AgentRunner
}

func NewService(runner agenteino.AgentRunner) *Service {
	return &Service{runner: runner}
}

func (s *Service) Execute(ctx context.Context, command Command) (Result, error) {
	command.SessionID = strings.TrimSpace(command.SessionID)
	command.Message = strings.TrimSpace(command.Message)

	if command.SessionID == "" {
		return Result{}, &ValidationError{Field: "session_id", Message: "session_id is required"}
	}
	if command.Message == "" {
		return Result{}, &ValidationError{Field: "message", Message: "message is required"}
	}
	if err := command.TimeContext.Validate(); err != nil {
		return Result{}, &ValidationError{Field: "time_context", Message: err.Error()}
	}

	agentOutput, err := s.runner.Run(ctx, agenteino.AgentInput{
		Message:     command.Message,
		TimeContext: command.TimeContext,
	})
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrExecution, err)
	}

	return Result{
		RequestID: command.RequestID,
		SessionID: command.SessionID,
		Agent:     agentOutput,
		TraceID:   "",
	}, nil
}
