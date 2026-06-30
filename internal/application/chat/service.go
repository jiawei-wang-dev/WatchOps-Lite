package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

var ErrExecution = errors.New("chat execution failed")

const (
	defaultRecentWindowSize = 12
	defaultSummaryThreshold = 12
)

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

type ServiceConfig struct {
	RecentWindowSize int
	SummaryThreshold int
}

type Service struct {
	runner           agenteino.AgentRunner
	store            session.Store
	summarizer       session.Summarizer
	recentWindowSize int
	summaryThreshold int
	now              func() time.Time
}

func NewService(
	runner agenteino.AgentRunner,
	store session.Store,
	summarizer session.Summarizer,
	config ServiceConfig,
) *Service {
	if config.RecentWindowSize <= 0 {
		config.RecentWindowSize = defaultRecentWindowSize
	}
	if config.SummaryThreshold <= 0 {
		config.SummaryThreshold = defaultSummaryThreshold
	}

	return &Service{
		runner:           runner,
		store:            store,
		summarizer:       summarizer,
		recentWindowSize: config.RecentWindowSize,
		summaryThreshold: config.SummaryThreshold,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
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

	snapshot, memoryAvailable := s.loadContext(ctx, command.SessionID)
	agentOutput, err := s.runner.Run(ctx, agenteino.AgentInput{
		SessionSummary: snapshot.Summary,
		RecentMessages: snapshot.RecentMessages,
		CurrentMessage: command.Message,
		TimeContext:    command.TimeContext,
	})
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrExecution, err)
	}
	ensureAgentMetadata(&agentOutput)
	agentOutput.Metadata["session_memory_available"] = memoryAvailable

	if !memoryAvailable {
		appendMemoryLimitation(&agentOutput)
	} else if err := s.persistContext(ctx, command, snapshot, agentOutput); err != nil {
		agentOutput.Metadata["session_memory_available"] = false
		appendMemoryLimitation(&agentOutput)
	}

	return Result{
		RequestID: command.RequestID,
		SessionID: command.SessionID,
		Agent:     agentOutput,
		TraceID:   "",
	}, nil
}

func (s *Service) loadContext(
	ctx context.Context,
	sessionID string,
) (session.ContextSnapshot, bool) {
	if s.store == nil {
		return emptySnapshot(), false
	}

	snapshot, err := s.store.LoadContext(ctx, sessionID)
	if err != nil {
		return emptySnapshot(), false
	}
	if snapshot.RecentMessages == nil {
		snapshot.RecentMessages = []session.Message{}
	}
	return snapshot, true
}

func (s *Service) persistContext(
	ctx context.Context,
	command Command,
	snapshot session.ContextSnapshot,
	agentOutput agenteino.AgentOutput,
) error {
	userMessage := session.Message{
		Role:      session.RoleUser,
		Content:   command.Message,
		CreatedAt: s.now(),
		RequestID: command.RequestID,
		Metadata: map[string]any{
			"time_range": map[string]any{
				"from": command.TimeContext.From,
				"to":   command.TimeContext.To,
			},
		},
	}
	assistantMessage := session.Message{
		Role:      session.RoleAssistant,
		Content:   assistantMemoryContent(agentOutput),
		CreatedAt: s.now(),
		RequestID: command.RequestID,
		Metadata:  assistantMemoryMetadata(agentOutput),
	}

	combined := make([]session.Message, 0, len(snapshot.RecentMessages)+2)
	combined = append(combined, snapshot.RecentMessages...)
	combined = append(combined, userMessage, assistantMessage)

	if len(combined) > s.summaryThreshold && len(combined) > s.recentWindowSize {
		if s.summarizer == nil {
			return errors.New("session summarizer is unavailable")
		}

		overflowCount := len(combined) - s.recentWindowSize
		updated, err := s.summarizer.Summarize(
			ctx,
			snapshot.Summary,
			combined[:overflowCount],
		)
		if err != nil {
			return err
		}
		if err := s.store.UpdateSummary(
			ctx,
			command.SessionID,
			updated,
			snapshot.Summary.Version,
		); err != nil {
			return err
		}
	}

	if err := s.store.AppendMessage(ctx, command.SessionID, userMessage); err != nil {
		return err
	}
	return s.store.AppendMessage(ctx, command.SessionID, assistantMessage)
}

func assistantMemoryContent(output agenteino.AgentOutput) string {
	parts := make([]string, 0, len(output.Conclusions)+len(output.Limitations))
	for _, conclusion := range output.Conclusions {
		if conclusion.Text != "" {
			parts = append(parts, conclusion.Text)
		}
	}
	if len(parts) == 0 {
		for _, limitation := range output.Limitations {
			if limitation.Message != "" {
				parts = append(parts, limitation.Message)
			}
		}
	}
	if len(parts) == 0 {
		return "No answer content was produced."
	}
	return strings.Join(parts, " ")
}

func assistantMemoryMetadata(output agenteino.AgentOutput) map[string]any {
	toolNames := make([]string, 0, len(output.ToolRuns))
	errorCodes := make([]string, 0, len(output.ToolRuns))
	for _, run := range output.ToolRuns {
		toolNames = append(toolNames, run.Tool)
		if run.ErrorCode != "" {
			errorCodes = append(errorCodes, string(run.ErrorCode))
		}
	}

	evidenceIDs := make([]string, 0, len(output.Evidence))
	resourceIDs := make([]string, 0, len(output.Evidence))
	services := make([]string, 0, len(output.Evidence))
	traceIDs := make([]string, 0, len(output.Evidence))
	for _, evidence := range output.Evidence {
		if evidence.ID != "" {
			evidenceIDs = append(evidenceIDs, evidence.ID)
		}
		if evidence.ResourceID != "" {
			resourceIDs = append(resourceIDs, evidence.ResourceID)
			switch evidence.SourceType {
			case "logs", "metrics":
				services = append(services, evidence.ResourceID)
			case "traces":
				traceIDs = append(traceIDs, evidence.ResourceID)
			}
		}
	}

	return map[string]any{
		"tool_names":   toolNames,
		"error_codes":  errorCodes,
		"evidence_ids": evidenceIDs,
		"resource_ids": resourceIDs,
		"services":     services,
		"trace_ids":    traceIDs,
	}
}

func appendMemoryLimitation(output *agenteino.AgentOutput) {
	for _, limitation := range output.Limitations {
		if limitation.Code == "SESSION_MEMORY_UNAVAILABLE" {
			return
		}
	}
	output.Limitations = append(output.Limitations, agenteino.Limitation{
		Code:    "SESSION_MEMORY_UNAVAILABLE",
		Message: "Session memory is unavailable; this response was generated without durable conversation context.",
	})
}

func ensureAgentMetadata(output *agenteino.AgentOutput) {
	if output.Metadata == nil {
		output.Metadata = map[string]any{}
	}
}

func emptySnapshot() session.ContextSnapshot {
	return session.ContextSnapshot{
		Summary:        session.EmptySummary(),
		RecentMessages: []session.Message{},
	}
}
