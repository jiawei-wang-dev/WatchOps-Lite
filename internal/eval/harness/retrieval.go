package harness

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/evaluation"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge/queryplan"
)

type offlineDocument struct {
	ID      string
	Title   string
	Content string
	Service string
}

type offlineSearcher struct {
	documents   []offlineDocument
	mode        string
	multi       bool
	conditional bool
	rerank      bool
}

func retrievalCases(dataset Dataset) ([]evaluation.Case, []offlineDocument) {
	cases := make([]evaluation.Case, 0, len(dataset.Scenarios))
	documents := []offlineDocument{}
	seen := map[string]struct{}{}
	for _, scenario := range dataset.Scenarios {
		service := scenario.ExpectedSlots["service"]
		if scenario.RetrievalExpectation != "skip" &&
			scenario.RetrievalExpectation != "miss" && len(scenario.RelevantDocumentIDs) > 0 {
			cases = append(cases, evaluation.Case{
				ID: scenario.CaseID, Query: scenario.Input, Service: service,
				RelevantDocumentIDs: append([]string{}, scenario.RelevantDocumentIDs...),
				ExpectedSourceType:  "knowledge",
			})
		}
	}
	documentScenarios := dataset.Scenarios
	if len(dataset.Corpus) > 0 {
		documentScenarios = dataset.Corpus
	}
	for _, scenario := range documentScenarios {
		service := scenario.ExpectedSlots["service"]
		for _, item := range scenario.Fixtures["search_knowledge"] {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			documents = append(documents, offlineDocument{
				ID: item.ID, Title: scenario.Category + " runbook",
				Content: item.Content, Service: service,
			})
		}
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].ID < documents[j].ID })
	return cases, documents
}

func (s offlineSearcher) Search(
	ctx context.Context,
	query string,
	limit int,
	filters map[string]string,
) ([]evaluation.SearchResult, error) {
	planInput := offlineQueryPlanInput(ctx, query, filters)
	authoritativeQuery := queryplan.AuthoritativeQuery(planInput)
	queries := []string{authoritativeQuery}
	useMultiQuery := s.multi
	if s.conditional {
		useMultiQuery = queryplan.ShouldUseMultiQuery(planInput)
	}
	if useMultiQuery {
		plan, err := queryplan.NewRuleBasedPlanner().Plan(ctx, planInput)
		if err == nil && len(plan.Queries) > 1 {
			queries = queries[:0]
			for _, item := range plan.Queries {
				queries = append(queries, item.Query)
			}
		} else {
			useMultiQuery = false
		}
	}
	type ranked struct {
		doc   offlineDocument
		score float64
	}
	rankedDocuments := make([]ranked, 0, len(s.documents))
	for _, document := range s.documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if filter := strings.TrimSpace(filters["service"]); filter != "" &&
			document.Service != "" && !sameService(filter, document.Service) {
			continue
		}
		score := 0.0
		for index, currentQuery := range queries {
			candidate := lexicalScore(currentQuery, document.Title+" "+document.Content)
			if s.mode == "hybrid" {
				candidate += semanticScore(currentQuery, document.Content)
			}
			candidate *= math.Max(0.55, 1-float64(index)*0.08)
			if candidate > score {
				score = candidate
			}
		}
		if s.rerank {
			score += lexicalScore(query, document.Title) * 1.5
			if strings.Contains(strings.ToLower(document.Title), "runbook") {
				score += 0.2
			}
		}
		rankedDocuments = append(rankedDocuments, ranked{doc: document, score: score})
	}
	sort.SliceStable(rankedDocuments, func(i, j int) bool {
		if rankedDocuments[i].score == rankedDocuments[j].score {
			return rankedDocuments[i].doc.ID < rankedDocuments[j].doc.ID
		}
		return rankedDocuments[i].score > rankedDocuments[j].score
	})
	totalCandidates := len(rankedDocuments)
	if limit <= 0 || limit > len(rankedDocuments) {
		limit = len(rankedDocuments)
	}
	results := make([]evaluation.SearchResult, 0, limit)
	for _, item := range rankedDocuments[:limit] {
		metadata := map[string]any{
			"retrieval_mode":   "offline_" + s.mode,
			"query_mode":       map[bool]string{true: "multi_query", false: "single_query"}[useMultiQuery],
			"total_candidates": totalCandidates,
		}
		if s.mode == "bm25" {
			metadata["bm25_score"] = item.score
		} else {
			metadata["hybrid_score"] = item.score
		}
		if s.rerank {
			metadata["rerank_provider"] = "rule_based"
			metadata["rerank_score"] = item.score
		}
		results = append(results, evaluation.SearchResult{
			DocumentID: item.doc.ID, Title: item.doc.Title,
			Content: item.doc.Content, Source: "fixture", Score: item.score,
			Metadata: metadata,
		})
	}
	return results, nil
}

func offlineQueryPlanInput(ctx context.Context, query string, filters map[string]string) queryplan.QueryPlanInput {
	recognized, _ := intent.NewRuleBasedRecognizer().Recognize(ctx, intent.RecognitionInput{Message: query})
	service := strings.TrimSpace(filters["service"])
	if service == "" {
		service = recognized.Service
	}
	return queryplan.QueryPlanInput{
		UserMessage: query, Intent: recognized, Service: service,
		Symptom: recognized.Symptom, Keywords: recognized.Keywords,
	}
}

func evaluateRetrieval(ctx context.Context, dataset Dataset) ([]RetrievalMetrics, MultiQueryMetrics, []BadCase) {
	cases, documents := retrievalCases(dataset)
	strategies := []offlineSearcher{
		{documents: documents, mode: "bm25"},
		{documents: documents, mode: "bm25", multi: true},
		{documents: documents, mode: "hybrid"},
		{documents: documents, mode: "hybrid", multi: true},
		{documents: documents, mode: "hybrid", multi: true, rerank: true},
		{documents: documents, mode: "hybrid", conditional: true, rerank: true},
	}
	metrics := make([]RetrievalMetrics, 0, len(strategies))
	badCases := []BadCase{}
	fixedNow, _ := timeFromDataset(dataset)
	for _, strategy := range strategies {
		report1 := evaluation.Evaluate(ctx, strategy, cases, 1)
		report3 := evaluation.Evaluate(ctx, strategy, cases, 3)
		report5 := evaluation.Evaluate(ctx, strategy, cases, 5)
		queryMode := "single_query"
		if strategy.multi {
			queryMode = "multi_query"
		}
		if strategy.conditional {
			queryMode = "conditional"
		}
		mode := strategy.mode
		if strategy.rerank {
			mode += "+rerank"
		}
		metrics = append(metrics, RetrievalMetrics{
			Mode: mode, QueryMode: queryMode, Cases: len(cases),
			RecallAt1: report1.RecallAtK, RecallAt3: report3.RecallAtK,
			RecallAt5: report5.RecallAtK, HitAt1: report1.HitRateAtK,
			HitAt3: report3.HitRateAtK, HitAt5: report5.HitRateAtK,
			MRR: report5.MRR, AverageCandidates: report5.AverageCandidateCount,
			AverageLatencyMS: report5.AverageLatencyMS,
		})
		if mode == "hybrid+rerank" && queryMode == "conditional" {
			for _, item := range report5.Cases {
				if item.Hit {
					continue
				}
				badCases = append(badCases, BadCase{
					CaseID: item.ID, Stage: "retrieval",
					Expected: "a relevant document in top 5",
					Actual:   item.TopKResultIDs, Reason: "no relevant document retrieved",
					Timestamp: fixedNow,
				})
			}
		}
	}
	return metrics, evaluateMultiQueryDecisions(ctx, cases, documents), badCases
}

func evaluateMultiQueryDecisions(ctx context.Context, cases []evaluation.Case, documents []offlineDocument) MultiQueryMetrics {
	result := MultiQueryMetrics{
		Cases: len(cases), TriggeredCaseIDs: []string{}, ImprovedCaseIDs: []string{},
		WorsenedCaseIDs: []string{}, NeutralCaseIDs: []string{},
	}
	singleCases, multiCases := []evaluation.Case{}, []evaluation.Case{}
	for _, item := range cases {
		input := offlineQueryPlanInput(ctx, item.Query, map[string]string{"service": item.Service})
		if queryplan.ShouldUseMultiQuery(input) {
			multiCases = append(multiCases, item)
			result.TriggeredCaseIDs = append(result.TriggeredCaseIDs, item.ID)
		} else {
			singleCases = append(singleCases, item)
		}
	}
	result.SingleQueryCases, result.MultiQueryCases = len(singleCases), len(multiCases)
	result.MultiQueryTriggerRate = ratio(len(multiCases), len(cases))
	singleSearcher := offlineSearcher{documents: documents, mode: "hybrid", rerank: true}
	multiSearcher := offlineSearcher{documents: documents, mode: "hybrid", multi: true, rerank: true}
	conditionalSearcher := offlineSearcher{documents: documents, mode: "hybrid", conditional: true, rerank: true}
	singleReport := evaluation.Evaluate(ctx, singleSearcher, singleCases, 5)
	multiReport := evaluation.Evaluate(ctx, multiSearcher, multiCases, 5)
	conditionalReport := evaluation.Evaluate(ctx, conditionalSearcher, cases, 5)
	result.SingleQueryRecallAt5, result.SingleQueryMRR = singleReport.RecallAtK, singleReport.MRR
	result.MultiQueryRecallAt5, result.MultiQueryMRR = multiReport.RecallAtK, multiReport.MRR
	result.ConditionalRecallAt5, result.ConditionalMRR = conditionalReport.RecallAtK, conditionalReport.MRR
	if len(multiCases) == 0 {
		return result
	}
	forcedSingle := evaluation.Evaluate(ctx, singleSearcher, multiCases, 5)
	forcedMulti := multiReport
	singleByID := caseResultsByID(forcedSingle.Cases)
	improved, worsened := 0, 0
	for _, item := range forcedMulti.Cases {
		before := singleByID[item.ID]
		switch {
		case item.ReciprocalRank > before.ReciprocalRank:
			improved++
			result.ImprovedCaseIDs = append(result.ImprovedCaseIDs, item.ID)
		case item.ReciprocalRank < before.ReciprocalRank:
			worsened++
			result.WorsenedCaseIDs = append(result.WorsenedCaseIDs, item.ID)
		default:
			result.NeutralCaseIDs = append(result.NeutralCaseIDs, item.ID)
		}
	}
	result.MultiQueryCorrectTriggerRate = ratio(improved, len(multiCases))
	result.IncorrectMultiQueryTriggerRate = ratio(worsened, len(multiCases))
	result.NeutralMultiQueryTriggerRate = ratio(len(multiCases)-improved-worsened, len(multiCases))
	return result
}

func caseResultsByID(values []evaluation.CaseResult) map[string]evaluation.CaseResult {
	result := make(map[string]evaluation.CaseResult, len(values))
	for _, item := range values {
		result[item.ID] = item
	}
	return result
}

func lexicalScore(query, content string) float64 {
	queryTerms := terms(query)
	contentTerms := terms(content)
	if len(queryTerms) == 0 {
		return 0
	}
	set := map[string]struct{}{}
	for _, term := range contentTerms {
		set[term] = struct{}{}
	}
	matches := 0
	for _, term := range queryTerms {
		if _, ok := set[term]; ok {
			matches++
		}
	}
	return float64(matches) / math.Sqrt(float64(len(queryTerms)))
}

func semanticScore(query, content string) float64 {
	groups := [][]string{
		{"error", "errors", "错误", "失败", "5xx", "502"},
		{"latency", "slow", "慢", "延迟", "耗时"},
		{"timeout", "deadline", "超时"},
		{"database", "db", "mysql", "连接", "connection", "pool"},
		{"trace", "span", "链路", "critical", "path"},
		{"runbook", "playbook", "手册", "guide", "处理"},
	}
	lowerQuery := strings.ToLower(query)
	lowerContent := strings.ToLower(content)
	score := 0.0
	for _, group := range groups {
		queryMatch, contentMatch := false, false
		for _, word := range group {
			queryMatch = queryMatch || strings.Contains(lowerQuery, word)
			contentMatch = contentMatch || strings.Contains(lowerContent, word)
		}
		if queryMatch && contentMatch {
			score += 0.75
		}
	}
	return score
}

func terms(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	})
}

func sameService(left, right string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.TrimSuffix(value, "-service")
		value = strings.TrimSuffix(value, "s")
		return value
	}
	return normalize(left) == normalize(right)
}
