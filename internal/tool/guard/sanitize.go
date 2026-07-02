package guard

import (
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
)

const RedactedValue = "[REDACTED]"

var sensitiveKeyParts = []string{
	"password",
	"token",
	"secret",
	"api_key",
	"authorization",
	"cookie",
	"credential",
}

func (g *Guard) SanitizeResult(result toolruntime.Result) toolruntime.Result {
	result.Payload = SanitizeMap(result.Payload)
	result.Metadata = SanitizeMap(result.Metadata)
	for index := range result.Evidence {
		result.Evidence[index] = SanitizeEvidence(result.Evidence[index])
	}
	if result.Error != nil {
		result.Error.Details = SanitizeMap(result.Error.Details)
	}
	return result
}

func SanitizeEvidence(item evidence.Item) evidence.Item {
	item.Metadata = SanitizeMap(item.Metadata)
	return item
}

func SanitizeMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		if sensitiveKey(key) {
			result[key] = RedactedValue
			continue
		}
		result[key] = sanitizeValue(value)
	}
	return result
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return SanitizeMap(typed)
	case map[string]string:
		result := make(map[string]string, len(typed))
		for key, current := range typed {
			if sensitiveKey(key) {
				result[key] = RedactedValue
			} else {
				result[key] = current
			}
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, sanitizeValue(item))
		}
		return result
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}
