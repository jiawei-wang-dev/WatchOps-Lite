package summary

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
)

const (
	maxSummaryContentLength = 4000
	maxStructuredItems      = 32
)

type Deterministic struct{}

func NewDeterministic() *Deterministic {
	return &Deterministic{}
}

func (d *Deterministic) Summarize(
	ctx context.Context,
	current session.Summary,
	messages []session.Message,
) (session.Summary, error) {
	if err := ctx.Err(); err != nil {
		return session.Summary{}, err
	}

	result := normalize(current)
	contentParts := make([]string, 0, len(messages)+1)
	if result.Content != "" {
		contentParts = append(contentParts, result.Content)
	}

	for _, message := range messages {
		if err := ctx.Err(); err != nil {
			return session.Summary{}, err
		}

		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		contentParts = append(contentParts, formatMessage(message, content))

		if message.Role == session.RoleUser {
			if result.Goal == "" {
				result.Goal = content
			}
			if strings.HasSuffix(content, "?") {
				result.OpenQuestions = appendUnique(result.OpenQuestions, content)
			}
		}
		if message.Role == session.RoleAssistant || message.Role == session.RoleTool {
			result.ConfirmedFacts = appendUnique(result.ConfirmedFacts, content)
		}

		if message.RequestID != "" {
			result.ImportantEntities = appendUnique(
				result.ImportantEntities,
				"request_id:"+message.RequestID,
			)
		}
		collectMetadata(&result, message.Metadata)
	}

	result.Content = trimToLast(strings.Join(contentParts, "\n"), maxSummaryContentLength)
	result.ConfirmedFacts = keepLast(result.ConfirmedFacts, maxStructuredItems)
	result.OpenQuestions = keepLast(result.OpenQuestions, maxStructuredItems)
	result.AttemptedActions = keepLast(result.AttemptedActions, maxStructuredItems)
	result.ImportantEntities = keepLast(result.ImportantEntities, maxStructuredItems)
	result.UpdatedAt = time.Now().UTC()
	return result, nil
}

func normalize(value session.Summary) session.Summary {
	if value.ConfirmedFacts == nil {
		value.ConfirmedFacts = []string{}
	}
	if value.OpenQuestions == nil {
		value.OpenQuestions = []string{}
	}
	if value.AttemptedActions == nil {
		value.AttemptedActions = []string{}
	}
	if value.ImportantEntities == nil {
		value.ImportantEntities = []string{}
	}
	return value
}

func formatMessage(message session.Message, content string) string {
	if message.RequestID == "" {
		return fmt.Sprintf("[%s] %s", message.Role, content)
	}
	return fmt.Sprintf("[%s][request_id=%s] %s", message.Role, message.RequestID, content)
}

func collectMetadata(result *session.Summary, metadata map[string]any) {
	for _, action := range metadataStrings(metadata, "tool_names") {
		result.AttemptedActions = appendUnique(result.AttemptedActions, action)
	}
	for _, code := range metadataStrings(metadata, "error_codes") {
		result.ConfirmedFacts = appendUnique(result.ConfirmedFacts, "tool_error:"+code)
	}
	for _, key := range []string{"services", "resource_ids", "trace_ids"} {
		for _, value := range metadataStrings(metadata, key) {
			result.ImportantEntities = appendUnique(result.ImportantEntities, value)
		}
	}

	timeRange, ok := metadata["time_range"].(map[string]any)
	if !ok {
		return
	}
	from, _ := timeRange["from"].(string)
	to, _ := timeRange["to"].(string)
	if from != "" || to != "" {
		result.ConfirmedFacts = appendUnique(
			result.ConfirmedFacts,
			"time_range:"+from+".."+to,
		)
	}
}

func metadataStrings(metadata map[string]any, key string) []string {
	value, ok := metadata[key]
	if !ok {
		return nil
	}

	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				result = append(result, text)
			}
		}
		return result
	case string:
		if typed != "" {
			return []string{typed}
		}
	}
	return nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func keepLast(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[len(values)-limit:]
}

func trimToLast(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

var _ session.Summarizer = (*Deterministic)(nil)
