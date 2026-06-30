package knowledge

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Chunker struct {
	maxSize int
}

func NewChunker(maxSize int) (*Chunker, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("%w: chunk max size must be positive", ErrInvalidArgument)
	}
	return &Chunker{maxSize: maxSize}, nil
}

func (c *Chunker) Split(document Document) []Chunk {
	paragraphs := splitParagraphs(document.Content)
	parts := make([]string, 0, len(paragraphs))
	current := ""

	flush := func() {
		if current != "" {
			parts = append(parts, current)
			current = ""
		}
	}

	for _, paragraph := range paragraphs {
		for _, piece := range splitLongText(paragraph, c.maxSize) {
			if current == "" {
				current = piece
				continue
			}
			candidate := current + "\n\n" + piece
			if utf8.RuneCountInString(candidate) <= c.maxSize {
				current = candidate
				continue
			}
			flush()
			current = piece
		}
	}
	flush()

	chunks := make([]Chunk, 0, len(parts))
	for index, content := range parts {
		chunks = append(chunks, Chunk{
			ID:         fmt.Sprintf("%s_chunk_%04d", document.ID, index),
			DocumentID: document.ID,
			Title:      document.Title,
			Content:    content,
			Source:     document.Source,
			Index:      index,
			Metadata:   cloneMetadata(document.Metadata),
			CreatedAt:  document.CreatedAt,
		})
	}
	return chunks
}

func splitParagraphs(content string) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	raw := strings.Split(normalized, "\n\n")
	paragraphs := make([]string, 0, len(raw))
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value != "" {
			paragraphs = append(paragraphs, value)
		}
	}
	return paragraphs
}

func splitLongText(value string, maxSize int) []string {
	if utf8.RuneCountInString(value) <= maxSize {
		return []string{value}
	}

	words := strings.Fields(value)
	if len(words) <= 1 {
		return splitRunes(value, maxSize)
	}

	parts := make([]string, 0)
	current := ""
	for _, word := range words {
		if utf8.RuneCountInString(word) > maxSize {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
			parts = append(parts, splitRunes(word, maxSize)...)
			continue
		}
		if current == "" {
			current = word
			continue
		}
		candidate := current + " " + word
		if utf8.RuneCountInString(candidate) <= maxSize {
			current = candidate
			continue
		}
		parts = append(parts, current)
		current = word
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func splitRunes(value string, maxSize int) []string {
	runes := []rune(value)
	parts := make([]string, 0, (len(runes)+maxSize-1)/maxSize)
	for start := 0; start < len(runes); start += maxSize {
		end := start + maxSize
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[start:end]))
	}
	return parts
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	copy := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copy[key] = value
	}
	return copy
}
