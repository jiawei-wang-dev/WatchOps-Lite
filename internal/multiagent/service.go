package multiagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/turngovernance"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
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
	timeout      time.Duration
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

func (s *Service) WithTimeout(timeout time.Duration) *Service {
	if timeout > 0 {
		s.timeout = timeout
	}
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
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
		span.SetAttributes(attribute.Int64("multiagent.timeout_ms", s.timeout.Milliseconds()))
	}

	outcome, err := turngovernance.NewResolver(
		s.sessionStore,
		s.orchestrator.recognizer,
		0.55,
	).WithNow(s.now).Resolve(ctx, turngovernance.TurnInput{
		RequestID:   command.RequestID,
		SessionID:   command.SessionID,
		UserID:      command.UserID,
		Message:     command.Message,
		TimeContext: command.TimeContext,
		Metadata:    command.Metadata,
	})
	if err != nil {
		observability.MarkError(span, "multi-agent turn governance failed")
		return Result{}, fmt.Errorf("%w: turn governance failed", ErrExecution)
	}
	if outcome.Decision.Decision == intent.DecisionClarify {
		output := buildClarificationResult(command, outcome)
		agenteino.EmitStreamEvent(ctx, "clarification_required", map[string]any{
			"intent_type":            outcome.ResolvedIntent.Intent,
			"confidence":             outcome.ResolvedIntent.Confidence,
			"missing_required_slots": outcome.Decision.MissingRequired,
			"reason_code":            outcome.Decision.ReasonCode,
			"pending_question":       outcome.Decision.ClarifyQuestion,
		})
		if err := s.persistFocus(
			ctx,
			command,
			outcome,
			output,
			session.TurnStatusClarify,
		); err != nil {
			runtimemetrics.IncSessionMemoryUnavailable()
			output.Metadata["persistence_failed"] = true
			output.Metadata["session_focus_persistence_failed"] = true
			output.FinalAnswer.Metadata["persistence_failed"] = true
		}
		traceID := observability.TraceID(ctx)
		if traceID != "" {
			output.Metadata["trace_id"] = traceID
			output.FinalAnswer.Metadata["trace_id"] = traceID
		}
		return Result{
			RequestID: command.RequestID,
			SessionID: command.SessionID,
			Output:    output,
			TraceID:   traceID,
		}, nil
	}

	// Intent Governance has declared the turn executable. AgentPlan can now
	// select roles, but it cannot change the validated task or slots.
	agentPlan := planAgentsWithTracing(ctx, outcome.ResolvedIntent)
	agenteino.EmitStreamEvent(ctx, "multiagent_plan_created", map[string]any{
		"intent_type":             string(outcome.ResolvedIntent.Intent),
		"selected_agents":         agentRoleStrings(agentPlan.SelectedAgents),
		"skipped_agents":          agentRoleStrings(agentPlan.SkippedAgents),
		"dynamic_routing_enabled": agentPlan.DynamicRoutingEnabled,
	})
	snapshot, memoryAvailable := s.loadSessionContext(ctx, command.SessionID)
	metadata := cloneServiceMetadata(command.Metadata)
	metadata["session_context"] = sessionContextForPrompt(snapshot)
	metadata["session_memory_available"] = memoryAvailable
	metadata["session_context_loaded"] = memoryAvailable
	metadata["recent_message_count"] = len(snapshot.RecentMessages)
	metadata["summary_version"] = snapshot.Summary.Version
	metadata["session_focus_available"] = outcome.MemoryAvailable
	metadata["decision"] = string(outcome.Decision.Decision)
	metadata["turn_governance_decision"] = string(outcome.Decision.Decision)

	output, err := s.orchestrator.Execute(ctx, Input{
		RequestID:   command.RequestID,
		SessionID:   command.SessionID,
		UserID:      command.UserID,
		Message:     command.Message,
		TimeContext: outcome.ResolvedTime,
		Metadata:    metadata,
		Intent:      outcome.ResolvedIntent,
		AgentPlan:   agentPlan,
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
	output.Metadata["session_focus_available"] = outcome.MemoryAvailable
	output.Metadata["decision"] = string(outcome.Decision.Decision)
	output.Metadata["turn_governance_decision"] = string(outcome.Decision.Decision)
	if memoryAvailable {
		persistCommand := command
		persistCommand.TimeContext = outcome.ResolvedTime
		if err := s.persistSessionContext(ctx, persistCommand, output); err != nil {
			output.Metadata["session_memory_available"] = false
			output.Metadata["session_persist_error"] = "session_memory_unavailable"
		}
	}
	if err := s.persistFocus(
		ctx,
		command,
		outcome,
		output,
		session.TurnStatusCompleted,
	); err != nil {
		runtimemetrics.IncSessionMemoryUnavailable()
		output.Metadata["session_focus_persistence_failed"] = true
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

func buildClarificationResult(
	command Command,
	outcome turngovernance.TurnOutcome,
) MultiAgentResult {
	metadata := map[string]any{
		"decision":                string(intent.DecisionClarify),
		"status":                  "clarification_required",
		"error_code":              "CLARIFICATION_REQUIRED",
		"intent_type":             string(outcome.ResolvedIntent.Intent),
		"confidence":              outcome.ResolvedIntent.Confidence,
		"reason_code":             outcome.Decision.ReasonCode,
		"missing_slots":           append([]string{}, outcome.Decision.MissingRequired...),
		"missing_required_slots":  append([]string{}, outcome.Decision.MissingRequired...),
		"known_slots":             turngovernance.CloneStringMap(outcome.Decision.KnownSlots),
		"pending_question":        outcome.Decision.ClarifyQuestion,
		"ambiguous_reference":     outcome.Decision.ReasonCode == "AMBIGUOUS_REFERENCE",
		"candidates":              append([]string{}, outcome.Focus.Candidates...),
		"session_id":              command.SessionID,
		"request_id":              command.RequestID,
		"session_focus_available": outcome.MemoryAvailable,
		"selected_agents":         []string{},
	}
	answerMetadata := cloneServiceMetadata(metadata)
	answer := agenteino.AgentOutput{
		Conclusions: []agenteino.Conclusion{{
			Text:        outcome.Decision.ClarifyQuestion,
			EvidenceIDs: []string{},
		}},
		Evidence:        []common.EvidenceItem{},
		Inferences:      []agenteino.Inference{},
		Recommendations: []agenteino.Recommendation{},
		Limitations: []agenteino.Limitation{{
			Code:    "CLARIFICATION_REQUIRED",
			Message: "关键信息不足，本轮未执行 Multi-Agent、RAG 或工具调用。",
		}},
		ToolRuns: []agenteino.ToolRun{},
		Metadata: answerMetadata,
	}
	return MultiAgentResult{
		Steps:       []AgentStep{},
		Evidence:    []common.EvidenceItem{},
		ToolRuns:    []agenteino.ToolRun{},
		FinalAnswer: answer,
		Metadata:    metadata,
	}
}

func (s *Service) persistFocus(
	ctx context.Context,
	command Command,
	outcome turngovernance.TurnOutcome,
	output MultiAgentResult,
	status string,
) error {
	return turngovernance.PersistFocus(ctx, s.sessionStore, turngovernance.FocusUpdate{
		SessionID:        command.SessionID,
		RequestID:        command.RequestID,
		Message:          command.Message,
		AssistantSummary: multiAgentAssistantMemoryContent(output),
		Status:           status,
		Focus:            outcome.Focus,
		Decision:         outcome.Decision,
		ResolvedIntent:   outcome.ResolvedIntent,
		EvidenceIDs:      multiAgentEvidenceIDs(output),
		Now:              s.now(),
	})
}

func multiAgentEvidenceIDs(output MultiAgentResult) []string {
	result := make([]string, 0, len(output.Evidence))
	for _, item := range output.Evidence {
		if item.ID != "" {
			result = append(result, item.ID)
		}
	}
	return result
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
