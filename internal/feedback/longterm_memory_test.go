package feedback

import (
	"context"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
)

type memoryWriterStub struct {
	saved []longterm.Memory
	err   error
}

func (s *memoryWriterStub) Save(_ context.Context, memory longterm.Memory) error {
	s.saved = append(s.saved, memory)
	return s.err
}

func (s *memoryWriterStub) Search(
	context.Context,
	longterm.SearchQuery,
) ([]longterm.Memory, error) {
	return nil, s.err
}

func (s *memoryWriterStub) Get(
	context.Context,
	string,
) (longterm.Memory, error) {
	return longterm.Memory{}, s.err
}

func TestPositiveFeedbackCreatesConfirmedLongTermMemory(t *testing.T) {
	feedbackStore := &storeStub{}
	memoryWriter := &memoryWriterStub{}
	service, err := NewServiceWithLongTermMemory(feedbackStore, memoryWriter)
	if err != nil {
		t.Fatalf("NewServiceWithLongTermMemory() error = %v", err)
	}
	service.newID = func() (string, error) { return "fb-confirmed", nil }

	_, err = service.Create(context.Background(), Feedback{
		RequestID:  "req-1",
		SessionID:  "ses-1",
		Rating:     RatingUp,
		ReasonTags: []string{"useful"},
		AnswerSnapshot: map[string]any{
			"conclusions": []any{
				map[string]any{"text": "Checkout timeouts followed payment latency."},
			},
		},
		EvidenceIDs: []string{"metric-1", "log-1"},
		Metadata:    map[string]any{"service": "checkout"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(memoryWriter.saved) != 1 {
		t.Fatalf("saved memories = %#v, want one", memoryWriter.saved)
	}
	memory := memoryWriter.saved[0]
	if memory.SourceType != longterm.SourceFeedbackUp ||
		memory.SourceID != "fb-confirmed" ||
		memory.Service != "checkout" ||
		len(memory.EvidenceIDs) != 2 {
		t.Fatalf("memory = %#v", memory)
	}
}

func TestNegativeFeedbackDoesNotCreateLongTermMemory(t *testing.T) {
	memoryWriter := &memoryWriterStub{}
	service, _ := NewServiceWithLongTermMemory(&storeStub{}, memoryWriter)
	service.newID = func() (string, error) { return "fb-down", nil }

	_, err := service.Create(context.Background(), Feedback{
		RequestID: "req-1",
		SessionID: "ses-1",
		Rating:    RatingDown,
		AnswerSnapshot: map[string]any{
			"conclusions": []any{"Unconfirmed root cause."},
		},
		EvidenceIDs: []string{"metric-1"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(memoryWriter.saved) != 0 {
		t.Fatalf("saved memories = %#v, want none", memoryWriter.saved)
	}
}
