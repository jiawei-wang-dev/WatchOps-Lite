package feedback

import (
	"fmt"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/longterm"
)

const maxConfirmedSummaryLength = 1000

func confirmedMemoryFromFeedback(value Feedback) (longterm.Memory, bool) {
	if value.Rating != RatingUp || len(value.EvidenceIDs) == 0 {
		return longterm.Memory{}, false
	}
	summary := confirmedSummary(value)
	if summary == "" {
		return longterm.Memory{}, false
	}
	service := feedbackServiceName(value)
	title := metadataString(value.Metadata, "title")
	if title == "" {
		title = "Confirmed incident memory"
		if service != "" {
			title += " for " + service
		}
	}
	return longterm.Memory{
		SourceType:  longterm.SourceFeedbackUp,
		SourceID:    value.ID,
		Service:     service,
		Title:       title,
		Summary:     truncateText(summary, maxConfirmedSummaryLength),
		EvidenceIDs: append([]string(nil), value.EvidenceIDs...),
		Tags:        append([]string(nil), value.ReasonTags...),
		Metadata: map[string]any{
			"request_id":   value.RequestID,
			"session_id":   value.SessionID,
			"confirmed_by": "positive_feedback",
		},
	}, true
}

func confirmedSummary(value Feedback) string {
	if corrected := strings.TrimSpace(value.CorrectedAnswer); corrected != "" {
		return corrected
	}
	for _, key := range []string{"conclusions", "summary", "answer"} {
		if text := snapshotText(value.AnswerSnapshot[key]); text != "" {
			return text
		}
	}
	return ""
}

func snapshotText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		return strings.Join(nonEmptyStrings(typed), " ")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := snapshotText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	case map[string]any:
		for _, key := range []string{"text", "summary", "content"} {
			if text := snapshotText(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func feedbackServiceName(value Feedback) string {
	if service := metadataString(value.Metadata, "service"); service != "" {
		return service
	}
	if service := metadataString(value.AnswerSnapshot, "service"); service != "" {
		return service
	}
	if metadata, ok := value.AnswerSnapshot["metadata"].(map[string]any); ok {
		if service := metadataString(metadata, "service"); service != "" {
			return service
		}
	}
	if evidence, ok := value.AnswerSnapshot["evidence"].([]any); ok {
		for _, item := range evidence {
			evidenceItem, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if metadata, ok := evidenceItem["metadata"].(map[string]any); ok {
				if service := metadataString(metadata, "service"); service != "" {
					return service
				}
			}
			sourceType := metadataString(evidenceItem, "source_type")
			if sourceType == "logs" || sourceType == "metrics" {
				if service := metadataString(evidenceItem, "resource_id"); service != "" {
					return service
				}
			}
		}
	}
	if services, ok := value.Metadata["services"].([]any); ok {
		for _, candidate := range services {
			if service, ok := candidate.(string); ok && strings.TrimSpace(service) != "" {
				return strings.TrimSpace(service)
			}
		}
	}
	return ""
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func truncateText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return fmt.Sprintf("%s…", string(runes[:limit-1]))
}
