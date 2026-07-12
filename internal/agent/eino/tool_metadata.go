package eino

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/alerts"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/topology"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/traces"
)

type toolCallMetadata struct {
	NormalizedArgs     string
	NormalizedArgsHash string
	Service            string
	TimeRange          *common.TimeRange
	ToolCategory       string
}

func extractToolCallMetadata(toolName string, arguments []byte) toolCallMetadata {
	metadata := toolCallMetadata{
		NormalizedArgs: strings.TrimSpace(string(arguments)),
		ToolCategory:   toolCategory(toolName),
	}
	var decoded any
	if err := json.Unmarshal(arguments, &decoded); err == nil {
		decoded = normalizeToolArguments(toolName, decoded)
		if normalized, err := json.Marshal(decoded); err == nil {
			metadata.NormalizedArgs = string(normalized)
		}
		if object, ok := decoded.(map[string]any); ok {
			metadata.Service = firstStringValue(object, "service", "service_name")
			metadata.TimeRange = extractTimeRange(object)
		}
	}
	metadata.NormalizedArgsHash = hashNormalizedArgs(metadata.NormalizedArgs)
	return metadata
}

func normalizeToolArgumentValue(value any) any {
	return normalizeToolArgumentValueForKey("", value)
}

func normalizeToolArguments(toolName string, value any) any {
	normalized := normalizeToolArgumentValue(value)
	object, ok := normalized.(map[string]any)
	if !ok {
		return normalized
	}
	applyToolArgumentDefaults(toolName, object)
	return object
}

func normalizeToolArgumentValueForKey(key string, value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := map[string]any{}
		for childKey, child := range typed {
			value := normalizeToolArgumentValueForKey(childKey, child)
			if isEmptyNormalizedArgument(value) {
				continue
			}
			normalized[childKey] = value
		}
		return normalized
	case []any:
		normalized := make([]any, 0, len(typed))
		for _, child := range typed {
			value := normalizeToolArgumentValueForKey(key, child)
			if isEmptyNormalizedArgument(value) {
				continue
			}
			normalized = append(normalized, value)
		}
		return normalized
	case string:
		return normalizeToolArgumentString(key, typed)
	case float64:
		if shouldDropZeroArgument(key) && typed == 0 {
			return nil
		}
		if typed == float64(int64(typed)) {
			return int64(typed)
		}
		return typed
	default:
		return value
	}
}

func normalizeToolArgumentString(key string, value string) string {
	normalized := collapseWhitespace(value)
	switch strings.ToLower(key) {
	case "service", "service_name", "metric_name", "level", "category", "source_type", "trace_id", "span_id":
		return strings.ToLower(normalized)
	case "query", "keyword", "message", "window":
		return strings.ToLower(normalized)
	default:
		return normalized
	}
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func isEmptyNormalizedArgument(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func shouldDropZeroArgument(key string) bool {
	switch strings.ToLower(key) {
	case "limit", "top_k", "topk":
		return true
	default:
		return false
	}
}

func applyToolArgumentDefaults(toolName string, values map[string]any) {
	defaultLimit := defaultToolLimit(toolName)
	if defaultLimit == 0 {
		return
	}
	for _, key := range []string{"limit", "top_k", "topK"} {
		if value, ok := values[key]; ok && fmt.Sprint(value) == fmt.Sprint(defaultLimit) {
			delete(values, key)
		}
	}
}

func defaultToolLimit(toolName string) int {
	switch toolName {
	case logs.Name, traces.Name:
		return 20
	case knowledge.Name:
		return 5
	default:
		return 0
	}
}

func hashNormalizedArgs(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func toolCategory(toolName string) string {
	switch toolName {
	case metrics.Name:
		return "metrics"
	case logs.Name:
		return "logs"
	case traces.Name:
		return "traces"
	case knowledge.Name:
		return "knowledge"
	case alerts.Name:
		return "alerts"
	case topology.Name:
		return "topology"
	default:
		return "unknown"
	}
}

func firstStringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractTimeRange(values map[string]any) *common.TimeRange {
	candidate, ok := values["time_range"]
	if !ok {
		candidate = values["timeContext"]
	}
	switch typed := candidate.(type) {
	case map[string]any:
		from, _ := typed["from"].(string)
		to, _ := typed["to"].(string)
		if strings.TrimSpace(from) != "" || strings.TrimSpace(to) != "" {
			return &common.TimeRange{From: strings.TrimSpace(from), To: strings.TrimSpace(to)}
		}
	case common.TimeRange:
		return &typed
	case *common.TimeRange:
		return typed
	}
	return nil
}

func enrichToolResultMetadata(result common.ToolResult, details toolCallMetadata) common.ToolResult {
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if details.NormalizedArgs != "" {
		result.Metadata["normalized_args"] = details.NormalizedArgs
	}
	if details.NormalizedArgsHash != "" {
		result.Metadata["normalized_args_hash"] = details.NormalizedArgsHash
	}
	if details.Service != "" {
		result.Metadata["service"] = details.Service
	}
	if details.TimeRange != nil {
		result.Metadata["time_range"] = map[string]any{
			"from": details.TimeRange.From,
			"to":   details.TimeRange.To,
		}
	}
	if details.ToolCategory != "" {
		result.Metadata["tool_category"] = details.ToolCategory
	}
	if _, ok := result.Metadata["deduplicated"]; !ok {
		result.Metadata["deduplicated"] = false
	}
	return result
}

func toolRunFromResult(toolName string, result common.ToolResult) ToolRun {
	if toolName == "" {
		toolName = result.Tool
	}
	metadata := result.Metadata
	run := ToolRun{
		Tool:            toolName,
		Success:         result.Success,
		DurationMS:      result.DurationMS,
		EvidenceCount:   len(result.Evidence),
		WarningCount:    len(result.Warnings),
		EvidenceIDs:     collectEvidenceIDs(result.Evidence),
		ExecutionStatus: "success",
		DataStatus:      toolResultDataStatus(result),
		FallbackUsed:    toolResultFallbackUsed(result),
		Metadata:        metadata,
	}
	if result.Tool != "" {
		run.Tool = result.Tool
	}
	run.NormalizedArgs, _ = metadata["normalized_args"].(string)
	run.NormalizedArgsHash, _ = metadata["normalized_args_hash"].(string)
	run.Service, _ = metadata["service"].(string)
	run.TimeRange = metadataTimeRange(metadata)
	run.ToolCategory, _ = metadata["tool_category"].(string)
	run.Deduplicated, _ = metadata["deduplicated"].(bool)
	run.ReusedResultFrom = metadataInt(metadata, "reused_result_from")
	run.RetryCount = metadataInt(metadata, "retry_count")
	run.RetryReason, _ = metadata["retry_reason"].(string)
	if result.Error != nil {
		run.ErrorCode = result.Error.Code
		run.ErrorMessage = result.Error.Message
		run.ExecutionStatus = "failed"
	}
	return run
}

func metadataTimeRange(metadata map[string]any) *common.TimeRange {
	if metadata == nil {
		return nil
	}
	switch value := metadata["time_range"].(type) {
	case map[string]any:
		from, _ := value["from"].(string)
		to, _ := value["to"].(string)
		if strings.TrimSpace(from) != "" || strings.TrimSpace(to) != "" {
			return &common.TimeRange{From: strings.TrimSpace(from), To: strings.TrimSpace(to)}
		}
	case common.TimeRange:
		return &value
	case *common.TimeRange:
		return value
	}
	return nil
}

func metadataInt(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	switch value := metadata[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}
