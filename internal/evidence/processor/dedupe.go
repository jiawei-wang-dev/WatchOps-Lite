package processor

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

func (p *Processor) dedupe(ctx context.Context, items []evidence.Item) []evidence.Item {
	_, span := observability.StartSpan(ctx, "evidence.dedupe")
	defer span.End()

	result := make([]evidence.Item, 0, len(items))
	seen := map[string]int{}
	for _, raw := range items {
		item := evidence.Normalize(raw, raw.Source)
		key := dedupeKey(item)
		if key == "" {
			key = "id:" + item.ID
		}
		if index, exists := seen[key]; exists {
			if betterRawEvidence(item, result[index]) {
				result[index] = item
			}
			continue
		}
		seen[key] = len(result)
		result = append(result, item)
	}
	span.SetAttributes(
		attribute.Int("evidence.input_count", len(items)),
		attribute.Int("evidence.output_count", len(result)),
	)
	return result
}

func dedupeKey(item evidence.Item) string {
	switch item.Source {
	case evidence.SourceKnowledge:
		return firstMetadataKey(item, "chunk_id", "content_hash", "document_id")
	case evidence.SourceLogs:
		if value := metadataString(item.Metadata, "log_id"); value != "" {
			return "log:" + value
		}
		return "log_hash:" + hashParts(item.Content, metadataString(item.Metadata, "timestamp"))
	case evidence.SourceTraces:
		traceID := firstNonEmpty(item.TraceID, metadataString(item.Metadata, "trace_id"))
		spanID := metadataString(item.Metadata, "span_id")
		if traceID != "" || spanID != "" {
			return "trace:" + traceID + ":" + spanID
		}
	case evidence.SourceMetrics:
		return "metric:" + strings.Join([]string{
			metadataString(item.Metadata, "metric_name"),
			item.ResourceID,
			metadataString(item.Metadata, "timestamp"),
		}, ":")
	}
	if item.ID != "" {
		return "id:" + item.ID
	}
	if item.Content != "" {
		return "content:" + hashParts(item.Content)
	}
	return ""
}

func betterRawEvidence(left, right evidence.Item) bool {
	if left.Score != nil && right.Score != nil && *left.Score != *right.Score {
		return *left.Score > *right.Score
	}
	if left.Score != nil && right.Score == nil {
		return true
	}
	return len(left.Content) > len(right.Content)
}

func firstMetadataKey(item evidence.Item, keys ...string) string {
	for _, key := range keys {
		if value := metadataString(item.Metadata, key); value != "" {
			return fmt.Sprintf("%s:%s", key, value)
		}
	}
	return ""
}

func hashParts(parts ...string) string {
	hash := sha1.New()
	for _, part := range parts {
		hash.Write([]byte(strings.TrimSpace(part)))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
