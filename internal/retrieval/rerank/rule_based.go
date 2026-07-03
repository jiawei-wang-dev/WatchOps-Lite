package rerank

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const ruleProvider = "rule_based"

type RuleBasedReranker struct {
	now func() time.Time
}

func NewRuleBased() *RuleBasedReranker {
	return &RuleBasedReranker{now: func() time.Time { return time.Now().UTC() }}
}

func (r *RuleBasedReranker) Rerank(
	ctx context.Context,
	query string,
	candidates []Candidate,
	topK int,
) ([]Result, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"retrieval.rerank.rule_based",
		attribute.Int("candidate_count", len(candidates)),
		attribute.Int("top_k", topK),
		attribute.String("provider", ruleProvider),
	)
	defer span.End()

	query = strings.TrimSpace(query)
	if query == "" {
		observability.MarkError(span, "rerank validation failed")
		return nil, fmt.Errorf("%w: query is required", ErrInvalidArgument)
	}
	limit, err := boundedTopK(topK, len(candidates))
	if err != nil {
		observability.MarkError(span, "rerank validation failed")
		return nil, err
	}
	if len(candidates) == 0 {
		return []Result{}, nil
	}

	queryTokens := meaningfulTokens(tokenize(query))
	queryTokenSet := tokenSet(queryTokens)
	results := make([]Result, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			observability.MarkError(span, "rule-based rerank canceled")
			return nil, err
		}
		score, reasons := r.score(query, queryTokens, queryTokenSet, candidate)
		results = append(results, Result{
			Candidate: candidate,
			Score:     score,
			Reason:    strings.Join(reasons, ", "),
			Provider:  ruleProvider,
		})
	}
	sort.SliceStable(results, func(left, right int) bool {
		if results[left].Score == results[right].Score {
			return stableCandidateID(results[left].Candidate) <
				stableCandidateID(results[right].Candidate)
		}
		return results[left].Score > results[right].Score
	})
	return results[:limit], nil
}

func (r *RuleBasedReranker) score(
	query string,
	queryTokens []string,
	queryTokenSet map[string]struct{},
	candidate Candidate,
) (float64, []string) {
	score := candidate.Score
	reasons := []string{"base_retrieval=" + formatScore(candidate.Score)}

	service := strings.ToLower(strings.TrimSpace(candidate.Service))
	if service != "" && containsToken(queryTokenSet, service) {
		score += 3
		reasons = append(reasons, "service_exact_match")
	}

	titleOverlap := overlapRatio(queryTokens, tokenize(candidate.Title))
	if titleOverlap > 0 {
		boost := titleOverlap * 2
		score += boost
		reasons = append(reasons, "title_overlap="+formatScore(boost))
	}
	contentOverlap := overlapRatio(queryTokens, tokenize(candidate.Content))
	if contentOverlap > 0 {
		score += contentOverlap
		reasons = append(reasons, "content_overlap="+formatScore(contentOverlap))
	}

	metadataMatches := metadataKeywordMatches(query, candidate.Metadata)
	if metadataMatches > 0 {
		boost := float64(metadataMatches) * 0.75
		score += boost
		reasons = append(
			reasons,
			"metadata_keyword_match="+strconv.Itoa(metadataMatches),
		)
	}

	identifierMatches := exactIdentifierMatches(queryTokens, candidate)
	if identifierMatches > 0 {
		boost := float64(identifierMatches) * 2.5
		score += boost
		reasons = append(reasons, "identifier_exact_match="+strconv.Itoa(identifierMatches))
	}

	if prefersRunbookQuery(query, queryTokenSet) && isRunbookCandidate(candidate) {
		score += 1.5
		reasons = append(reasons, "runbook_preference")
	}

	if !candidate.CreatedAt.IsZero() {
		age := r.now().Sub(candidate.CreatedAt)
		switch {
		case age < 0:
		case age <= 30*24*time.Hour:
			score += 0.5
			reasons = append(reasons, "recent_30d")
		case age <= 180*24*time.Hour:
			score += 0.25
			reasons = append(reasons, "recent_180d")
		}
	}
	if strings.TrimSpace(candidate.Content) == "" {
		score -= 5
		reasons = append(reasons, "empty_content_penalty")
	}
	return score, reasons
}

func tokenize(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(character rune) bool {
		return !unicode.IsLetter(character) &&
			!unicode.IsDigit(character) &&
			character != '_' &&
			character != '-' &&
			character != '.' &&
			character != ':'
	})
}

func meaningfulTokens(values []string) []string {
	stopWords := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "are": {}, "for": {}, "from": {},
		"in": {}, "is": {}, "of": {}, "on": {}, "the": {}, "to": {},
		"with": {},
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.Trim(value, ".:")
		if len(value) < 2 {
			continue
		}
		if _, skip := stopWords[value]; skip {
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

func tokenSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func containsToken(values map[string]struct{}, candidate string) bool {
	if _, ok := values[candidate]; ok {
		return true
	}
	for value := range values {
		if strings.EqualFold(strings.TrimSuffix(value, "-service"), candidate) ||
			strings.EqualFold(value, candidate+"-service") {
			return true
		}
	}
	return false
}

func overlapRatio(queryTokens, candidateTokens []string) float64 {
	if len(queryTokens) == 0 {
		return 0
	}
	candidateSet := tokenSet(meaningfulTokens(candidateTokens))
	matches := 0
	for _, token := range queryTokens {
		if _, ok := candidateSet[token]; ok {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTokens))
}

func exactIdentifierMatches(queryTokens []string, candidate Candidate) int {
	candidateTokens := tokenSet(tokenize(strings.Join([]string{
		candidate.Title,
		candidate.Content,
		candidate.Service,
		candidate.DocumentID,
		candidate.ChunkID,
	}, " ")))
	matches := 0
	for _, token := range queryTokens {
		if !looksLikeIdentifier(token) {
			continue
		}
		if _, ok := candidateTokens[token]; ok {
			matches++
		}
	}
	return matches
}

func looksLikeIdentifier(value string) bool {
	if len(value) >= 16 ||
		strings.ContainsAny(value, "_-:") ||
		strings.HasPrefix(value, "err") ||
		strings.HasPrefix(value, "trace") ||
		strings.HasPrefix(value, "req") {
		return true
	}
	if len(value) >= 3 {
		for _, character := range value {
			if !unicode.IsDigit(character) {
				return false
			}
		}
		return true
	}
	return false
}

func prefersRunbook(queryTokens map[string]struct{}) bool {
	for _, value := range []string{"runbook", "knowledge", "mitigation", "procedure"} {
		if _, ok := queryTokens[value]; ok {
			return true
		}
	}
	return false
}

func prefersRunbookQuery(
	query string,
	queryTokens map[string]struct{},
) bool {
	return prefersRunbook(queryTokens) ||
		strings.Contains(query, "排查") ||
		strings.Contains(query, "怎么处理") ||
		strings.Contains(query, "如何处理")
}

func metadataKeywordMatches(query string, metadata map[string]any) int {
	query = strings.ToLower(query)
	values := make([]string, 0)
	switch keywords := metadata["keywords"].(type) {
	case []string:
		values = append(values, keywords...)
	case []any:
		for _, value := range keywords {
			if text, ok := value.(string); ok {
				values = append(values, text)
			}
		}
	}
	matches := 0
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || !strings.Contains(query, value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		matches++
	}
	return matches
}

func isRunbookCandidate(candidate Candidate) bool {
	value := strings.ToLower(strings.Join([]string{
		candidate.SourceType,
		candidate.Source,
		candidate.Title,
	}, " "))
	return strings.Contains(value, "runbook") || strings.Contains(value, "knowledge")
}

func stableCandidateID(candidate Candidate) string {
	if candidate.ID != "" {
		return candidate.ID
	}
	if candidate.ChunkID != "" {
		return candidate.ChunkID
	}
	return candidate.DocumentID
}

func formatScore(value float64) string {
	return strconv.FormatFloat(value, 'f', 4, 64)
}
