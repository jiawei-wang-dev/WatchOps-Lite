package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type Case struct {
	ID                 string   `json:"id"`
	Query              string   `json:"query"`
	Service            string   `json:"service,omitempty"`
	ExpectedDocID      string   `json:"expected_doc_id,omitempty"`
	ExpectedChunkID    string   `json:"expected_chunk_id,omitempty"`
	ExpectedKeywords   []string `json:"expected_keywords"`
	ExpectedSourceType string   `json:"expected_source_type"`
	Notes              string   `json:"notes,omitempty"`
}

type SearchResult struct {
	ChunkID    string         `json:"chunk_id"`
	DocumentID string         `json:"document_id"`
	Title      string         `json:"title"`
	Content    string         `json:"content"`
	Source     string         `json:"source"`
	Score      float64        `json:"score"`
	Metadata   map[string]any `json:"metadata"`
}

type Searcher interface {
	Search(ctx context.Context, query string, limit int, filters map[string]string) ([]SearchResult, error)
}

type CaseResult struct {
	ID                 string         `json:"id"`
	Query              string         `json:"query"`
	RetrievalMode      string         `json:"retrieval_mode,omitempty"`
	TopKResultIDs      []string       `json:"top_k_result_ids"`
	MatchedKeywords    []string       `json:"matched_keywords"`
	Hit                bool           `json:"hit"`
	EmptyRecall        bool           `json:"empty_recall"`
	BM25Score          *float64       `json:"bm25_score,omitempty"`
	VectorScore        *float64       `json:"vector_score,omitempty"`
	HybridScore        *float64       `json:"hybrid_score,omitempty"`
	RRFScore           *float64       `json:"rrf_score,omitempty"`
	ExpectedSourceType string         `json:"expected_source_type"`
	Error              string         `json:"error,omitempty"`
	Notes              string         `json:"notes,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type Report struct {
	Total    int          `json:"total"`
	Passed   int          `json:"passed"`
	Failed   int          `json:"failed"`
	PassRate float64      `json:"pass_rate"`
	Cases    []CaseResult `json:"cases"`
}

func LoadCases(reader io.Reader) ([]Case, error) {
	var cases []Case
	if err := json.NewDecoder(reader).Decode(&cases); err != nil {
		return nil, fmt.Errorf("decode retrieval eval cases: %w", err)
	}
	for index, current := range cases {
		if strings.TrimSpace(current.ID) == "" {
			return nil, fmt.Errorf("case[%d].id is required", index)
		}
		if strings.TrimSpace(current.Query) == "" {
			return nil, fmt.Errorf("case[%d].query is required", index)
		}
		if strings.TrimSpace(current.ExpectedSourceType) == "" {
			return nil, fmt.Errorf("case[%d].expected_source_type is required", index)
		}
	}
	return cases, nil
}

func Evaluate(
	ctx context.Context,
	searcher Searcher,
	cases []Case,
	topK int,
) Report {
	if topK <= 0 {
		topK = 5
	}
	report := Report{Total: len(cases), Cases: make([]CaseResult, 0, len(cases))}
	for _, current := range cases {
		result := evaluateCase(ctx, searcher, current, topK)
		if result.Hit {
			report.Passed++
		} else {
			report.Failed++
		}
		report.Cases = append(report.Cases, result)
	}
	if report.Total > 0 {
		report.PassRate = float64(report.Passed) / float64(report.Total)
	}
	return report
}

func evaluateCase(
	ctx context.Context,
	searcher Searcher,
	current Case,
	topK int,
) CaseResult {
	result := CaseResult{
		ID:                 current.ID,
		Query:              current.Query,
		ExpectedSourceType: current.ExpectedSourceType,
		Notes:              current.Notes,
		TopKResultIDs:      []string{},
		MatchedKeywords:    []string{},
		Metadata:           map[string]any{},
	}
	if searcher == nil {
		result.Error = "searcher is unavailable"
		result.EmptyRecall = true
		return result
	}
	filters := map[string]string{}
	if current.Service != "" {
		filters["service"] = current.Service
	}
	results, err := searcher.Search(ctx, current.Query, topK, filters)
	if err != nil {
		result.Error = err.Error()
		result.EmptyRecall = true
		return result
	}
	if len(results) == 0 {
		result.EmptyRecall = true
		return result
	}
	result.TopKResultIDs = topKIDs(results)
	result.MatchedKeywords = MatchKeywords(current.ExpectedKeywords, results)
	result.RetrievalMode = retrievalMode(results[0])
	result.BM25Score = scoreField(results[0], "bm25_score")
	result.VectorScore = scoreField(results[0], "vector_score")
	result.RRFScore = scoreField(results[0], "rrf_score")
	result.HybridScore = scoreField(results[0], "hybrid_score")
	if result.HybridScore == nil {
		result.HybridScore = result.RRFScore
	}
	result.Metadata["top_score"] = results[0].Score
	result.Hit = isHit(current, results, result.MatchedKeywords)
	return result
}

func MatchKeywords(expected []string, results []SearchResult) []string {
	if len(expected) == 0 || len(results) == 0 {
		return []string{}
	}
	combined := strings.ToLower(searchableText(results))
	matched := make([]string, 0, len(expected))
	seen := map[string]struct{}{}
	for _, keyword := range expected {
		normalized := strings.ToLower(strings.TrimSpace(keyword))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		if strings.Contains(combined, normalized) {
			matched = append(matched, normalized)
			seen[normalized] = struct{}{}
		}
	}
	sort.Strings(matched)
	return matched
}

func isHit(current Case, results []SearchResult, matchedKeywords []string) bool {
	if current.ExpectedChunkID != "" || current.ExpectedDocID != "" {
		for _, result := range results {
			if current.ExpectedChunkID != "" && result.ChunkID == current.ExpectedChunkID {
				return true
			}
			if current.ExpectedDocID != "" && result.DocumentID == current.ExpectedDocID {
				return true
			}
		}
		return false
	}
	if len(current.ExpectedKeywords) == 0 {
		return len(results) > 0
	}
	return len(matchedKeywords) == len(uniqueKeywords(current.ExpectedKeywords))
}

func topKIDs(results []SearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		if result.ChunkID != "" {
			ids = append(ids, result.ChunkID)
			continue
		}
		if result.DocumentID != "" {
			ids = append(ids, result.DocumentID)
		}
	}
	return ids
}

func searchableText(results []SearchResult) string {
	var builder strings.Builder
	for _, result := range results {
		builder.WriteString(result.Title)
		builder.WriteByte('\n')
		builder.WriteString(result.Content)
		builder.WriteByte('\n')
		builder.WriteString(result.DocumentID)
		builder.WriteByte('\n')
		builder.WriteString(result.ChunkID)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func retrievalMode(result SearchResult) string {
	if value, ok := stringMetadata(result.Metadata, "retrieval_mode"); ok {
		return value
	}
	if nested, ok := result.Metadata["metadata"].(map[string]any); ok {
		if value, ok := stringMetadata(nested, "retrieval_mode"); ok {
			return value
		}
	}
	return ""
}

func stringMetadata(metadata map[string]any, key string) (string, bool) {
	value, ok := metadata[key].(string)
	return value, ok && strings.TrimSpace(value) != ""
}

func scoreField(result SearchResult, key string) *float64 {
	if score, ok := numericMetadata(result.Metadata, key); ok {
		return &score
	}
	if nested, ok := result.Metadata["metadata"].(map[string]any); ok {
		if score, ok := numericMetadata(nested, key); ok {
			return &score
		}
	}
	return nil
}

func numericMetadata(metadata map[string]any, key string) (float64, bool) {
	switch value := metadata[key].(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	default:
		return 0, false
	}
}

func uniqueKeywords(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
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
