package multiagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
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
	sessionStore session.Store
	now          func() time.Time
}

func NewService(orchestrator *Orchestrator) *Service {
	return &Service{
		orchestrator: orchestrator,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) WithSessionMemory(store session.Store) *Service {
	s.sessionStore = store
	return s
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

	snapshot, memoryAvailable := s.loadSessionContext(ctx, command.SessionID)
	metadata := cloneServiceMetadata(command.Metadata)
	metadata["session_context"] = sessionContextForPrompt(snapshot)
	metadata["session_memory_available"] = memoryAvailable
	metadata["session_context_loaded"] = memoryAvailable
	metadata["recent_message_count"] = len(snapshot.RecentMessages)
	metadata["summary_version"] = snapshot.Summary.Version

	output, err := s.orchestrator.Execute(ctx, Input{
		RequestID:   command.RequestID,
		SessionID:   command.SessionID,
		UserID:      command.UserID,
		Message:     command.Message,
		TimeContext: command.TimeContext,
		Metadata:    metadata,
	})
	if err != nil {
		observability.MarkError(span, "multi-agent workflow failed")
		return Result{}, err
	}
	traceID := observability.TraceID(ctx)
	if output.Metadata == nil {
		output.Metadata = map[string]any{}
	}
	output.Metadata["session_memory_available"] = memoryAvailable
	output.Metadata["session_context_loaded"] = memoryAvailable
	output.Metadata["recent_message_count"] = len(snapshot.RecentMessages)
	output.Metadata["summary_version"] = snapshot.Summary.Version
	if memoryAvailable {
		if err := s.persistSessionContext(ctx, command, output); err != nil {
			output.Metadata["session_memory_available"] = false
			output.Metadata["session_persist_error"] = "session_memory_unavailable"
		}
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

func (s *Service) loadSessionContext(
	ctx context.Context,
	sessionID string,
) (session.ContextSnapshot, bool) {
	if s.sessionStore == nil {
		return emptySessionContext(), false
	}
	snapshot, err := s.sessionStore.LoadContext(ctx, sessionID)
	if err != nil {
		return emptySessionContext(), false
	}
	if snapshot.RecentMessages == nil {
		snapshot.RecentMessages = []session.Message{}
	}
	return snapshot, true
}

func (s *Service) persistSessionContext(
	ctx context.Context,
	command Command,
	output MultiAgentResult,
) error {
	if s.sessionStore == nil {
		return nil
	}
	userMessage := session.Message{
		Role:      session.RoleUser,
		Content:   command.Message,
		CreatedAt: s.now(),
		RequestID: command.RequestID,
	}
	if err := s.sessionStore.AppendMessage(ctx, command.SessionID, userMessage); err != nil {
		return err
	}
	assistantMessage := session.Message{
		Role:      session.RoleAssistant,
		Content:   multiAgentAssistantMemoryContent(output),
		CreatedAt: s.now(),
		RequestID: command.RequestID,
		Metadata: map[string]any{
			"agent_mode": "multi_agent",
		},
	}
	return s.sessionStore.AppendMessage(ctx, command.SessionID, assistantMessage)
}

func sessionContextForPrompt(snapshot session.ContextSnapshot) map[string]any {
	return map[string]any{
		"summary":              snapshot.Summary,
		"recent_messages":      snapshot.RecentMessages,
		"recent_message_count": len(snapshot.RecentMessages),
		"summary_version":      snapshot.Summary.Version,
	}
}

func emptySessionContext() session.ContextSnapshot {
	return session.ContextSnapshot{
		Summary:        session.EmptySummary(),
		RecentMessages: []session.Message{},
	}
}

func cloneServiceMetadata(metadata map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range metadata {
		result[key] = value
	}
	return result
}

func multiAgentAssistantMemoryContent(output MultiAgentResult) string {
	parts := []string{}
	for _, conclusion := range output.FinalAnswer.Conclusions {
		if text := strings.TrimSpace(conclusion.Text); text != "" {
			parts = append(parts, text)
		}
	}
	for _, inference := range output.FinalAnswer.Inferences {
		if text := strings.TrimSpace(inference.Text); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		for _, limitation := range output.FinalAnswer.Limitations {
			if text := strings.TrimSpace(limitation.Message); text != "" {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) == 0 {
		return "Multi-Agent completed without a text conclusion."
	}
	return strings.Join(parts, "\n")
}
