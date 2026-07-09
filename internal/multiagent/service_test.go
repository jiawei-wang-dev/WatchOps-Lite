package multiagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type serviceSessionStore struct {
	snapshot session.ContextSnapshot
	loadErr  error
	appended []session.Message
}

func (s *serviceSessionStore) AppendMessage(
	_ context.Context,
	_ string,
	message session.Message,
) error {
	s.appended = append(s.appended, message)
	return nil
}

func (s *serviceSessionStore) GetRecentMessages(
	context.Context,
	string,
	int,
) ([]session.Message, error) {
	return s.snapshot.RecentMessages, nil
}

func (s *serviceSessionStore) GetSummary(
	context.Context,
	string,
) (session.Summary, error) {
	return s.snapshot.Summary, nil
}

func (s *serviceSessionStore) UpdateSummary(
	context.Context,
	string,
	session.Summary,
	int64,
) error {
	return nil
}

func (s *serviceSessionStore) LoadContext(
	context.Context,
	string,
) (session.ContextSnapshot, error) {
	if s.loadErr != nil {
		return session.ContextSnapshot{}, s.loadErr
	}
	return s.snapshot, nil
}

func (s *serviceSessionStore) ClearHistory(context.Context, string) error {
	return nil
}

func TestServiceLoadsAndPersistsSessionMemory(t *testing.T) {
	store := &serviceSessionStore{snapshot: session.ContextSnapshot{
		Summary: session.Summary{Content: "Previous checkout context", Version: 3},
		RecentMessages: []session.Message{{
			Role:    session.RoleUser,
			Content: "Previous question",
		}},
	}}
	service := NewService(testOrchestrator(t)).WithSessionMemory(store)
	service.now = func() time.Time {
		return time.Date(2026, 7, 9, 1, 2, 3, 0, time.UTC)
	}

	result, err := service.Execute(context.Background(), Command{
		RequestID: "req-memory",
		SessionID: "ses-memory",
		Message:   "Why is checkout failing?",
		TimeContext: common.TimeRange{
			From: "2026-07-09T00:00:00Z",
			To:   "2026-07-09T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Output.Metadata["session_memory_available"] != true ||
		result.Output.Metadata["session_context_loaded"] != true ||
		result.Output.Metadata["recent_message_count"] != 1 ||
		result.Output.Metadata["summary_version"] != int64(3) {
		t.Fatalf("metadata = %#v", result.Output.Metadata)
	}
	if len(store.appended) != 2 ||
		store.appended[0].Role != session.RoleUser ||
		store.appended[1].Role != session.RoleAssistant {
		t.Fatalf("appended = %#v", store.appended)
	}
}

func TestServiceDegradesWhenSessionMemoryUnavailable(t *testing.T) {
	store := &serviceSessionStore{loadErr: errors.New("redis unavailable")}
	service := NewService(testOrchestrator(t)).WithSessionMemory(store)

	result, err := service.Execute(context.Background(), Command{
		RequestID: "req-memory-down",
		SessionID: "ses-memory-down",
		Message:   "Why is checkout failing?",
		TimeContext: common.TimeRange{
			From: "2026-07-09T00:00:00Z",
			To:   "2026-07-09T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Output.Metadata["session_memory_available"] != false ||
		result.Output.Metadata["session_context_loaded"] != false {
		t.Fatalf("metadata = %#v", result.Output.Metadata)
	}
	if len(store.appended) != 0 {
		t.Fatalf("appended = %#v, want none", store.appended)
	}
}

func testOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	orchestrator := NewOrchestrator(
		context.Background(),
		fakeTriagePlanner{},
		staticAnalyzer{finding: AgentFinding{
			Role:        AgentRoleEvidence,
			Summary:     "evidence summary",
			EvidenceIDs: []string{"evidence-1"},
			Evidence: []common.EvidenceItem{{
				ID:         "evidence-1",
				SourceType: "metrics",
				Content:    "checkout error rate elevated",
			}},
		}},
		staticAnalyzer{finding: AgentFinding{
			Role:        AgentRoleKnowledge,
			Summary:     "knowledge summary",
			EvidenceIDs: []string{"knowledge-1"},
			Evidence: []common.EvidenceItem{{
				ID:         "knowledge-1",
				SourceType: "knowledge",
				Content:    "checkout runbook",
			}},
		}},
		fakeSynthesizer{},
	)
	return orchestrator
}
