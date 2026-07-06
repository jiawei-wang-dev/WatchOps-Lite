package chat

import (
	"context"
	"errors"
	"strings"
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/profile"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

var ErrExecution = errors.New("chat execution failed")

const (
	defaultRecentWindowSize = 12
	defaultSummaryThreshold = 12
)

type Command struct {
	RequestID   string
	SessionID   string
	UserID      string
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
	RecentWindowSize   int
	SummaryThreshold   int
	LongTermMemory     longterm.Store
	LongTermMemoryTopK int
	ProfileLoader      profile.Loader
	KnowledgeRetriever KnowledgeRetriever
	PreRAGTopK         int
}

type Service struct {
	runner             agenteino.AgentRunner
	store              session.Store
	summarizer         session.Summarizer
	recentWindowSize   int
	summaryThreshold   int
	now                func() time.Time
	graph              chatGraphRunner
	graphErr           error
	longTermMemory     longterm.Store
	longTermMemoryTopK int
	profileLoader      profile.Loader
	knowledgeRetriever KnowledgeRetriever
	preRAGTopK         int
}

type KnowledgeRetriever interface {
	HybridRetrieve(context.Context, retrievalknowledge.RetrievalRequest) (retrievalknowledge.RetrievalResult, error)
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

	service := &Service{
		runner:             runner,
		store:              store,
		summarizer:         summarizer,
		recentWindowSize:   config.RecentWindowSize,
		summaryThreshold:   config.SummaryThreshold,
		longTermMemory:     config.LongTermMemory,
		longTermMemoryTopK: config.LongTermMemoryTopK,
		profileLoader:      config.ProfileLoader,
		knowledgeRetriever: config.KnowledgeRetriever,
		preRAGTopK:         config.PreRAGTopK,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
	if service.preRAGTopK <= 0 {
		service.preRAGTopK = 5
	}
	service.graph, service.graphErr = compileChatGraph(context.Background(), service)
	return service
}

func (s *Service) Execute(ctx context.Context, command Command) (Result, error) {
	started := time.Now()
	success := false
	defer func() {
		runtimemetrics.ObserveChat(success, time.Since(started))
	}()
	ctx, span := observability.StartSpan(
		ctx,
		"chat.execute",
		attribute.String("request_id", command.RequestID),
		attribute.String("session_id", command.SessionID),
		attribute.Int("message_length", len(command.Message)),
		attribute.String("time_context.from", command.TimeContext.From),
		attribute.String("time_context.to", command.TimeContext.To),
	)
	defer span.End()

	command.SessionID = strings.TrimSpace(command.SessionID)
	command.UserID = strings.TrimSpace(command.UserID)
	command.Message = strings.TrimSpace(command.Message)

	if command.SessionID == "" {
		observability.MarkError(span, "chat validation failed")
		return Result{}, &ValidationError{Field: "session_id", Message: "session_id is required"}
	}
	if command.Message == "" {
		observability.MarkError(span, "chat validation failed")
		return Result{}, &ValidationError{Field: "message", Message: "message is required"}
	}
	if len(command.UserID) > 128 {
		observability.MarkError(span, "chat validation failed")
		return Result{}, &ValidationError{Field: "user_id", Message: "user_id exceeds 128 characters"}
	}
	if err := command.TimeContext.Validate(); err != nil {
		observability.MarkError(span, "chat validation failed")
		return Result{}, &ValidationError{Field: "time_context", Message: err.Error()}
	}

	result, err := s.executeGraph(ctx, command)
	if err != nil {
		observability.MarkError(span, "chat workflow failed")
		return Result{}, err
	}
	success = true
	return result, nil
}

func (s *Service) loadContext(
	ctx context.Context,
	sessionID string,
) (session.ContextSnapshot, bool) {
	ctx, span := observability.StartSpan(
		ctx,
		"session.load_context",
		attribute.String("session_id", sessionID),
	)
	defer span.End()

	if s.store == nil {
		runtimemetrics.IncSessionMemoryUnavailable()
		span.SetAttributes(attribute.Bool("memory_available", false))
		observability.MarkError(span, "session memory unavailable")
		return emptySnapshot(), false
	}

	snapshot, err := s.store.LoadContext(ctx, sessionID)
	if err != nil {
		runtimemetrics.IncSessionMemoryUnavailable()
		span.SetAttributes(attribute.Bool("memory_available", false))
		observability.MarkError(span, "session memory load failed")
		return emptySnapshot(), false
	}
	if snapshot.RecentMessages == nil {
		snapshot.RecentMessages = []session.Message{}
	}
	span.SetAttributes(
		attribute.Bool("memory_available", true),
		attribute.Int("recent_message_count", len(snapshot.RecentMessages)),
		attribute.Int64("summary_version", snapshot.Summary.Version),
	)
	return snapshot, true
}

func (s *Service) persistContext(
	ctx context.Context,
	command Command,
	snapshot session.ContextSnapshot,
	agentOutput agenteino.AgentOutput,
) error {
	ctx, span := observability.StartSpan(
		ctx,
		"session.persist_context",
		attribute.String("session_id", command.SessionID),
		attribute.Bool("summary_updated", false),
	)
	defer span.End()

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
			observability.MarkError(span, "session summarizer unavailable")
			return errors.New("session summarizer is unavailable")
		}

		overflowCount := len(combined) - s.recentWindowSize
		summaryContext, summarySpan := observability.StartSpan(
			ctx,
			"session.update_summary",
			attribute.String("session_id", command.SessionID),
			attribute.Int("message_count", overflowCount),
			attribute.Int64("summary_version", snapshot.Summary.Version),
		)
		updated, err := s.summarizer.Summarize(
			summaryContext,
			snapshot.Summary,
			combined[:overflowCount],
		)
		if err != nil {
			observability.MarkError(summarySpan, "session summary failed")
			summarySpan.End()
			observability.MarkError(span, "session persistence failed")
			return err
		}
		if err := s.store.UpdateSummary(
			summaryContext,
			command.SessionID,
			updated,
			snapshot.Summary.Version,
		); err != nil {
			observability.MarkError(summarySpan, "session summary persistence failed")
			summarySpan.End()
			observability.MarkError(span, "session persistence failed")
			return err
		}
		summarySpan.SetAttributes(attribute.Bool("summary_updated", true))
		summarySpan.End()
		span.SetAttributes(attribute.Bool("summary_updated", true))
	}

	if err := s.store.AppendMessage(ctx, command.SessionID, userMessage); err != nil {
		observability.MarkError(span, "session persistence failed")
		return err
	}
	if err := s.store.AppendMessage(ctx, command.SessionID, assistantMessage); err != nil {
		observability.MarkError(span, "session persistence failed")
		return err
	}
	span.SetAttributes(
		attribute.Bool("memory_available", true),
		attribute.Int("recent_message_count", len(combined)),
	)
	return nil
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
