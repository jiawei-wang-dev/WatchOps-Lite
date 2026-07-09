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

func TestPositiveFeedbackCreatesMemoryFromConclusionArray(t *testing.T) {
	memory, ok := confirmedMemoryFromFeedback(Feedback{
		ID:        "fb-conclusion",
		RequestID: "req-1",
		SessionID: "ses-1",
		Rating:    RatingUp,
		AnswerSnapshot: map[string]any{
			"conclusion": []any{
				map[string]any{"text": "checkout 错误率升高与 payment 超时证据一致。"},
			},
		},
		EvidenceIDs: []string{"metric-1"},
		Metadata:    map[string]any{"service": "checkout"},
	})
	if !ok {
		t.Fatal("confirmedMemoryFromFeedback() ok = false, want true")
	}
	if memory.Summary != "checkout 错误率升高与 payment 超时证据一致。" ||
		memory.Service != "checkout" ||
		len(memory.EvidenceIDs) != 1 {
		t.Fatalf("memory = %#v", memory)
	}
}

func TestCorrectedAnswerTakesPriorityForConfirmedMemory(t *testing.T) {
	memory, ok := confirmedMemoryFromFeedback(Feedback{
		ID:              "fb-corrected",
		Rating:          RatingUp,
		CorrectedAnswer: "Operator-confirmed corrected answer.",
		AnswerSnapshot: map[string]any{
			"conclusion": []any{
				map[string]any{"text": "Old answer."},
			},
		},
		EvidenceIDs: []string{"metric-1"},
	})
	if !ok {
		t.Fatal("confirmedMemoryFromFeedback() ok = false, want true")
	}
	if memory.Summary != "Operator-confirmed corrected answer." {
		t.Fatalf("summary = %q", memory.Summary)
	}
}

func TestPositiveFeedbackWithoutEvidenceDoesNotCreateConfirmedMemory(t *testing.T) {
	if _, ok := confirmedMemoryFromFeedback(Feedback{
		ID:     "fb-no-evidence",
		Rating: RatingUp,
		AnswerSnapshot: map[string]any{
			"conclusion": []any{
				map[string]any{"text": "Useful but not evidence-bound."},
			},
		},
	}); ok {
		t.Fatal("confirmedMemoryFromFeedback() ok = true, want false")
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
