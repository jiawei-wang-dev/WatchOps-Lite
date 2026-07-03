package rerank

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRuleBasedRerankerSignals(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		candidates []Candidate
		wantFirst  string
		wantReason string
	}{
		{
			name:  "service exact match",
			query: "checkout latency",
			candidates: []Candidate{
				{ID: "other", Service: "payment", Content: "latency", Score: 1},
				{ID: "checkout", Service: "checkout", Content: "latency", Score: 1},
			},
			wantFirst:  "checkout",
			wantReason: "service_exact_match",
		},
		{
			name:  "title keyword overlap",
			query: "checkout timeout runbook",
			candidates: []Candidate{
				{ID: "generic", Title: "General operations", Content: "reference", Score: 1},
				{ID: "specific", Title: "Checkout timeout runbook", Content: "reference", Score: 1},
			},
			wantFirst:  "specific",
			wantReason: "title_overlap",
		},
		{
			name:  "empty content penalty",
			query: "checkout",
			candidates: []Candidate{
				{ID: "empty", Title: "Checkout", Content: "", Score: 2},
				{ID: "complete", Title: "Checkout", Content: "Inspect saturation.", Score: 1},
			},
			wantFirst:  "complete",
			wantReason: "title_overlap",
		},
		{
			name:  "exact operational identifier",
			query: "find trace-1234567890abcdef",
			candidates: []Candidate{
				{ID: "unmatched", Content: "Another trace", Score: 1},
				{ID: "matched", Content: "Trace trace-1234567890abcdef timed out.", Score: 1},
			},
			wantFirst:  "matched",
			wantReason: "identifier_exact_match",
		},
		{
			name:  "Chinese metadata keyword overlap",
			query: "payment 支付超时怎么排查",
			candidates: []Candidate{
				{ID: "generic", Title: "General notes", Content: "payment", Score: 1},
				{
					ID:      "zh-runbook",
					Title:   "Checkout 服务排障 Runbook",
					Content: "payment 支付依赖超时",
					Score:   1,
					Metadata: map[string]any{
						"keywords": []any{"payment", "支付", "超时"},
					},
				},
			},
			wantFirst:  "zh-runbook",
			wantReason: "metadata_keyword_match",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reranker := NewRuleBased()
			results, err := reranker.Rerank(
				context.Background(),
				test.query,
				test.candidates,
				len(test.candidates),
			)
			if err != nil {
				t.Fatalf("Rerank() error = %v", err)
			}
			if results[0].Candidate.ID != test.wantFirst ||
				!strings.Contains(results[0].Reason, test.wantReason) {
				t.Fatalf("results = %#v", results)
			}
		})
	}
}

func TestRuleBasedRerankerIsDeterministicAndBoundsTopK(t *testing.T) {
	reranker := NewRuleBased()
	reranker.now = func() time.Time {
		return time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	}
	candidates := []Candidate{
		{ID: "b", Title: "Checkout runbook", Content: "Inspect timeout.", Score: 1},
		{ID: "a", Title: "Checkout runbook", Content: "Inspect timeout.", Score: 1},
	}
	first, err := reranker.Rerank(context.Background(), "checkout timeout", candidates, 10)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	second, err := reranker.Rerank(context.Background(), "checkout timeout", candidates, 10)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if len(first) != 2 || first[0].Candidate.ID != "a" ||
		first[0].Candidate.ID != second[0].Candidate.ID ||
		first[0].Score != second[0].Score {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if _, err := reranker.Rerank(context.Background(), "checkout", candidates, 0); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("top_k=0 error = %v, want ErrInvalidArgument", err)
	}
}

func TestCompositeRerankerFallsBackOnExternalFailure(t *testing.T) {
	composite, err := NewComposite(
		rerankerStub{err: ErrUnavailable},
		NewRuleBased(),
	)
	if err != nil {
		t.Fatalf("NewComposite() error = %v", err)
	}
	results, err := composite.Rerank(
		context.Background(),
		"checkout runbook",
		[]Candidate{{ID: "runbook", Title: "Checkout runbook", Content: "Inspect latency.", Score: 1}},
		1,
	)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if len(results) != 1 ||
		results[0].Provider != ruleProvider ||
		results[0].FallbackReason != "external_unavailable" {
		t.Fatalf("results = %#v", results)
	}
}

func TestCompositeRerankerFallsBackOnEmptyExternalResult(t *testing.T) {
	composite, _ := NewComposite(rerankerStub{}, NewRuleBased())
	results, err := composite.Rerank(
		context.Background(),
		"checkout",
		[]Candidate{{ID: "one", Content: "checkout", Score: 1}},
		1,
	)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if results[0].FallbackReason != "external_empty_result" {
		t.Fatalf("results = %#v", results)
	}
}

func TestCompositeRerankerClassifiesTimeoutFallback(t *testing.T) {
	composite, _ := NewComposite(
		rerankerStub{err: context.DeadlineExceeded},
		NewRuleBased(),
	)
	results, err := composite.Rerank(
		context.Background(),
		"checkout",
		[]Candidate{{ID: "one", Content: "checkout", Score: 1}},
		1,
	)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if results[0].FallbackReason != "external_timeout" {
		t.Fatalf("results = %#v", results)
	}
}

func TestCompositeRerankerClassifiesInvalidResponseFallback(t *testing.T) {
	composite, _ := NewComposite(
		rerankerStub{err: ErrInvalidResponse},
		NewRuleBased(),
	)
	results, err := composite.Rerank(
		context.Background(),
		"checkout",
		[]Candidate{{ID: "one", Content: "checkout", Score: 1}},
		1,
	)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if results[0].FallbackReason != "external_invalid_response" {
		t.Fatalf("results = %#v", results)
	}
}

type rerankerStub struct {
	results []Result
	err     error
}

func (s rerankerStub) Rerank(
	context.Context,
	string,
	[]Candidate,
	int,
) ([]Result, error) {
	return s.results, s.err
}
