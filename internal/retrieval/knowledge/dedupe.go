package knowledge

import (
	"fmt"
	"sort"
)

const (
	searchFingerprintRunes = 1000
	maxHiddenDuplicateIDs  = 10
)

type dedupeIdentity struct {
	Key    string
	Reason string
}

func dedupeSearchResults(results []SearchResult) []SearchResult {
	if len(results) < 2 {
		return cloneSearchResults(results)
	}
	sorted := cloneSearchResults(results)
	sort.SliceStable(sorted, func(left, right int) bool {
		return resultRankScore(sorted[left]) > resultRankScore(sorted[right])
	})

	seen := make(map[string]int, len(sorted)*3)
	unique := make([]SearchResult, 0, len(sorted))
	for _, result := range sorted {
		identities := resultDedupeIdentities(result)
		duplicateIndex := -1
		duplicateIdentity := dedupeIdentity{}
		for _, identity := range identities {
			if index, ok := seen[identity.Key]; ok {
				duplicateIndex = index
				duplicateIdentity = identity
				break
			}
		}
		if duplicateIndex >= 0 {
			recordHiddenDuplicate(
				&unique[duplicateIndex],
				result,
				duplicateIdentity,
			)
			continue
		}
		result.Metadata = cloneMetadata(result.Metadata)
		unique = append(unique, result)
		index := len(unique) - 1
		for _, identity := range identities {
			seen[identity.Key] = index
		}
	}
	return unique
}

func resultDedupeIdentities(result SearchResult) []dedupeIdentity {
	identities := make([]dedupeIdentity, 0, 4)
	if result.ChunkID != "" {
		identities = append(identities, dedupeIdentity{
			Key:    "chunk_id:" + result.ChunkID,
			Reason: "chunk_id",
		})
	}
	if result.DocumentID != "" {
		identities = append(identities, dedupeIdentity{
			Key: fmt.Sprintf(
				"document_chunk:%s:%d",
				result.DocumentID,
				result.ChunkIndex,
			),
			Reason: "document_id_chunk_index",
		})
	}
	if contentHash := metadataString(result.Metadata, "content_hash"); contentHash != "" {
		identities = append(identities, dedupeIdentity{
			Key: fmt.Sprintf(
				"content_hash:%s:%d",
				contentHash,
				result.ChunkIndex,
			),
			Reason: "content_hash",
		})
	}
	if fingerprint := searchResultFingerprint(result); fingerprint != "" {
		identities = append(identities, dedupeIdentity{
			Key:    "content_fingerprint:" + fingerprint,
			Reason: "content_fingerprint",
		})
	}
	return identities
}

func searchResultFingerprint(result SearchResult) string {
	normalized := normalizeKnowledgeContent(result.Title + "\n" + result.Content)
	runes := []rune(normalized)
	if len(runes) > searchFingerprintRunes {
		normalized = string(runes[:searchFingerprintRunes])
	}
	if normalized == "" {
		return ""
	}
	return ContentHash(normalized)
}

func resultRankScore(result SearchResult) float64 {
	if score, ok := metadataFloat(result.Metadata, "rerank_score"); ok {
		return score
	}
	return result.Score
}

func metadataFloat(metadata map[string]any, key string) (float64, bool) {
	switch value := metadata[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

func recordHiddenDuplicate(
	kept *SearchResult,
	duplicate SearchResult,
	identity dedupeIdentity,
) {
	kept.Metadata = cloneMetadata(kept.Metadata)
	count, _ := metadataFloat(kept.Metadata, "deduped_duplicate_count")
	kept.Metadata["deduped_duplicate_count"] = int(count) + 1
	kept.Metadata["dedupe_key"] = identity.Key
	kept.Metadata["dedupe_reason"] = identity.Reason

	hidden := metadataStrings(kept.Metadata, "hidden_duplicate_ids")
	duplicateID := duplicate.ChunkID
	if duplicateID == "" {
		duplicateID = duplicate.DocumentID
	}
	if duplicateID != "" &&
		len(hidden) < maxHiddenDuplicateIDs &&
		!containsString(hidden, duplicateID) {
		hidden = append(hidden, duplicateID)
	}
	if len(hidden) > 0 {
		kept.Metadata["hidden_duplicate_ids"] = hidden
	}
}

func metadataStrings(metadata map[string]any, key string) []string {
	switch values := metadata[key].(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func cloneSearchResults(results []SearchResult) []SearchResult {
	cloned := append([]SearchResult(nil), results...)
	for index := range cloned {
		cloned[index].Metadata = cloneMetadata(cloned[index].Metadata)
	}
	return cloned
}
