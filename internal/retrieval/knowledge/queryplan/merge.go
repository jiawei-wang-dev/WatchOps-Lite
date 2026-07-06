package queryplan

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
)

func MergeResults(
	plan RAGQueryPlan,
	results []RAGSubQueryResult,
	topK int,
) retrievalknowledge.RetrievalResult {
	if topK <= 0 {
		topK = 5
	}
	merged := map[string]retrievalknowledge.RetrievedKnowledge{}
	weights := map[string]float64{}
	types := map[string][]string{}
	successfulQueries := 0
	for _, current := range results {
		if current.Error != nil {
			continue
		}
		successfulQueries++
		for _, chunk := range current.Result.Chunks {
			key := dedupeKey(chunk)
			if key == "" {
				continue
			}
			score := weightedScore(chunk, current.Query.Weight)
			existing, exists := merged[key]
			if !exists || score > weights[key] {
				chunk.Metadata = cloneMetadata(chunk.Metadata)
				chunk.Metadata["rag_sub_query_type"] = string(current.Query.Type)
				chunk.Metadata["rag_sub_query_weight"] = current.Query.Weight
				chunk.Metadata["rag_sub_query_reason"] = current.Query.Reason
				merged[key] = chunk
				weights[key] = score
			} else {
				existing.Metadata = cloneMetadata(existing.Metadata)
				merged[key] = existing
			}
			types[key] = append(types[key], string(current.Query.Type))
		}
	}
	chunks := make([]retrievalknowledge.RetrievedKnowledge, 0, len(merged))
	for key, chunk := range merged {
		chunk.Metadata = cloneMetadata(chunk.Metadata)
		chunk.Metadata["rag_matched_sub_queries"] = dedupeStrings(types[key])
		chunk.Metadata["rag_multi_query_score"] = weights[key]
		chunk.Score = weights[key]
		chunks = append(chunks, chunk)
	}
	sort.SliceStable(chunks, func(i int, j int) bool {
		if chunks[i].Score == chunks[j].Score {
			return chunks[i].ID < chunks[j].ID
		}
		return chunks[i].Score > chunks[j].Score
	})
	if len(chunks) > topK {
		chunks = chunks[:topK]
	}
	return retrievalknowledge.RetrievalResult{
		Chunks: chunks,
		Metadata: map[string]any{
			"rag_query_plan_source":      plan.Source,
			"rag_sub_query_count":        len(plan.Queries),
			"rag_sub_query_types":        queryTypes(plan.Queries),
			"query_rewrite_applied":      len(plan.Queries) > 1,
			"query_plan_fallback_used":   metadataBool(plan.Metadata, "query_plan_fallback_used"),
			"rag_successful_query_count": successfulQueries,
			"selected_chunk_count":       len(chunks),
		},
	}
}

func dedupeKey(chunk retrievalknowledge.RetrievedKnowledge) string {
	if chunk.ChunkID != "" {
		return "chunk:" + chunk.ChunkID
	}
	if value, _ := chunk.Metadata["content_hash"].(string); value != "" {
		return "hash:" + value
	}
	if chunk.DocumentID != "" {
		return "doc:" + chunk.DocumentID
	}
	if strings.TrimSpace(chunk.Content) != "" {
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(strings.TrimSpace(chunk.Content)))
		return "content:" + strconv.FormatUint(hash.Sum64(), 10)
	}
	return ""
}

func weightedScore(chunk retrievalknowledge.RetrievedKnowledge, weight float64) float64 {
	score := chunk.Score
	if chunk.RerankScore > 0 {
		score += chunk.RerankScore
	}
	if chunk.FusedScore > 0 {
		score += chunk.FusedScore
	}
	if score == 0 {
		score = 1
	}
	if weight <= 0 {
		weight = 0.5
	}
	return score * weight
}

func cloneMetadata(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func dedupeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func metadataBool(metadata map[string]any, key string) bool {
	value, _ := metadata[key].(bool)
	return value
}
