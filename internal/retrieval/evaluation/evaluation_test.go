package evaluation

import (
	"context"
	"os"
	"strings"
	"testing"
)

type fakeSearcher struct {
	results []SearchResult
	err     error
}

func (s fakeSearcher) Search(
	context.Context,
	string,
	int,
	map[string]string,
) ([]SearchResult, error) {
	return s.results, s.err
}

func TestLoadCases(t *testing.T) {
	cases, err := LoadCases(strings.NewReader(`[
		{
			"id":"checkout-timeout",
			"query":"checkout payment timeout",
			"expected_keywords":["checkout","payment","timeout"],
			"expected_source_type":"knowledge"
		}
	]`))
	if err != nil {
		t.Fatalf("LoadCases() error = %v", err)
	}
	if len(cases) != 1 || cases[0].ID != "checkout-timeout" {
		t.Fatalf("cases = %#v", cases)
	}
}

func TestMatchKeywords(t *testing.T) {
	matched := MatchKeywords(
		[]string{"checkout", "payment", "timeout"},
		[]SearchResult{{
			Title:   "Checkout Runbook",
			Content: "Payment dependency timeout handling.",
		}},
	)
	if strings.Join(matched, ",") != "checkout,payment,timeout" {
		t.Fatalf("matched = %#v", matched)
	}
}

func TestChineseRetrievalEvalCasesAreValidAndMatchBilingualRunbook(t *testing.T) {
	file, err := os.Open("../../../testdata/retrieval_eval_cases_zh.json")
	if err != nil {
		t.Fatalf("open Chinese retrieval cases: %v", err)
	}
	defer file.Close()
	cases, err := LoadCases(file)
	if err != nil {
		t.Fatalf("LoadCases() error = %v", err)
	}
	if len(cases) != 3 {
		t.Fatalf("Chinese case count = %d, want 3", len(cases))
	}
	runbook := SearchResult{
		Title: "Checkout 服务高错误率排障 Runbook",
		Content: "checkout payment timeout error rate retry amplification " +
			"支付依赖 超时 错误率 重试放大",
	}
	for _, current := range cases {
		matched := MatchKeywords(
			current.ExpectedKeywords,
			[]SearchResult{runbook},
		)
		if len(matched) != len(current.ExpectedKeywords) {
			t.Fatalf(
				"case %s matched %v, want %v",
				current.ID,
				matched,
				current.ExpectedKeywords,
			)
		}
	}
}

func TestEvaluateHandlesEmptyRecall(t *testing.T) {
	report := Evaluate(context.Background(), fakeSearcher{}, []Case{{
		ID:                 "empty",
		Query:              "no match",
		ExpectedKeywords:   []string{"missing"},
		ExpectedSourceType: "knowledge",
	}}, 3)
	if report.Total != 1 || report.Passed != 0 || !report.Cases[0].EmptyRecall {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateHandlesEmptyCases(t *testing.T) {
	report := Evaluate(context.Background(), fakeSearcher{}, nil, 3)
	if report.Total != 0 || report.Passed != 0 || report.Failed != 0 || report.PassRate != 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateReportsHitAndScores(t *testing.T) {
	report := Evaluate(context.Background(), fakeSearcher{results: []SearchResult{{
		ChunkID:    "doc_checkout_chunk_0001",
		DocumentID: "doc_checkout",
		Title:      "Checkout Service High Error Rate Runbook",
		Content:    "Checkout payment timeout investigation.",
		Score:      1.2,
		Metadata: map[string]any{
			"retrieval_mode":  "hybrid",
			"bm25_score":      1.2,
			"rrf_score":       0.04,
			"rerank_provider": "rule_based",
			"rerank_score":    2.4,
			"rerank_reason":   "title_overlap",
		},
	}}}, []Case{{
		ID:                 "checkout",
		Query:              "checkout payment timeout",
		ExpectedKeywords:   []string{"checkout", "payment", "timeout"},
		ExpectedSourceType: "knowledge",
	}}, 3)
	if report.Passed != 1 ||
		report.Cases[0].RetrievalMode != "hybrid" ||
		report.Cases[0].BM25Score == nil ||
		report.Cases[0].RRFScore == nil ||
		report.Cases[0].RerankProvider != "rule_based" ||
		report.Cases[0].RerankScore == nil ||
		report.Cases[0].RerankReason != "title_overlap" {
		t.Fatalf("report = %#v", report)
	}
}
