package intent

import (
	"strings"

	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
)

func BuildRetrievalRequest(message string, result IntentResult, defaultTopK int) retrievalknowledge.RetrievalRequest {
	topK := defaultTopK
	if result.RAGHints.TopKOverride > 0 && result.RAGHints.TopKOverride <= 20 {
		topK = result.RAGHints.TopKOverride
	}
	if topK <= 0 {
		topK = 5
	}
	queryParts := []string{strings.TrimSpace(message)}
	if result.Symptom != "" {
		queryParts = append(queryParts, result.Symptom)
	}
	queryParts = append(queryParts, result.Keywords...)
	queryParts = append(queryParts, result.RAGHints.QueryBoosts...)
	filters := map[string]string{}
	if len(result.RAGHints.Categories) == 1 {
		filters["category"] = result.RAGHints.Categories[0]
	}
	return retrievalknowledge.RetrievalRequest{
		Query:   strings.Join(dedupeStrings(queryParts), " "),
		TopK:    topK,
		Filters: filters,
	}
}

func ShouldRunPreRAG(result IntentResult) bool {
	switch result.Intent {
	case IntentGeneralChat:
		return result.Confidence >= 0.7
	case IntentMetricsQuery, IntentLogsQuery:
		return len(result.RAGHints.Categories) > 0 && result.Confidence >= 0.75
	default:
		return true
	}
}
