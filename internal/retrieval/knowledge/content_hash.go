package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(normalizeKnowledgeContent(content)))
	return hex.EncodeToString(sum[:])
}

func normalizeKnowledgeContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(strings.TrimSpace(content), "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = stripMarkdownHeadingMarker(strings.TrimSpace(line))
		fields := strings.FieldsFunc(line, unicode.IsSpace)
		if len(fields) > 0 {
			normalized = append(normalized, asciiLower(strings.Join(fields, " ")))
		}
	}
	return strings.Join(normalized, " ")
}

func stripMarkdownHeadingMarker(value string) string {
	index := 0
	for index < len(value) && value[index] == '#' {
		index++
	}
	if index > 0 && index < len(value) &&
		(value[index] == ' ' || value[index] == '\t') {
		return strings.TrimSpace(value[index:])
	}
	return value
}

func asciiLower(value string) string {
	return strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		return character
	}, value)
}
