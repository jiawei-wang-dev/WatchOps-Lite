package knowledge

import (
	"testing"
	"time"
	"unicode/utf8"
)

func TestChunkerUsesParagraphsAndStableIDs(t *testing.T) {
	chunker, err := NewChunker(24)
	if err != nil {
		t.Fatalf("NewChunker() error = %v", err)
	}
	document := Document{
		ID:        "doc_test",
		Title:     "Runbook",
		Source:    "manual",
		Content:   "First paragraph.\n\nSecond paragraph is longer.\n\nThird.",
		Metadata:  map[string]any{"category": "runbook"},
		CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}

	first := chunker.Split(document)
	second := chunker.Split(document)

	if len(first) != len(second) || len(first) < 2 {
		t.Fatalf("chunk count = %d and %d, want stable multiple chunks", len(first), len(second))
	}
	for index := range first {
		if first[index].ID != second[index].ID {
			t.Fatalf("chunk ID changed: %q != %q", first[index].ID, second[index].ID)
		}
		if utf8.RuneCountInString(first[index].Content) > 24 {
			t.Fatalf("chunk %q exceeds max size", first[index].Content)
		}
	}
	if first[0].ID != "doc_test_chunk_0000" {
		t.Fatalf("first chunk ID = %q", first[0].ID)
	}
}

func TestChunkerSplitsLongUnbrokenText(t *testing.T) {
	chunker, _ := NewChunker(4)
	chunks := chunker.Split(Document{ID: "doc_unicode", Content: "可靠性分析助手"})

	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	for _, chunk := range chunks {
		if utf8.RuneCountInString(chunk.Content) > 4 {
			t.Fatalf("chunk %q exceeds rune limit", chunk.Content)
		}
	}
}
