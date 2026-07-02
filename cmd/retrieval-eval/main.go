package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/evaluation"
)

func main() {
	var (
		casesPath = flag.String("cases", "testdata/retrieval_eval_cases.json", "path to retrieval eval cases JSON")
		baseURL   = flag.String("base-url", "http://localhost:8080", "WatchOps-Lite API base URL")
		topK      = flag.Int("top-k", 5, "number of results to request per case")
		timeout   = flag.Duration("timeout", 10*time.Second, "per-request timeout")
		output    = flag.String("output", "", "optional JSON report output path")
	)
	flag.Parse()

	file, err := os.Open(*casesPath)
	if err != nil {
		exitf("open cases: %v", err)
	}
	defer file.Close()
	cases, err := evaluation.LoadCases(file)
	if err != nil {
		exitf("%v", err)
	}

	searcher, err := evaluation.NewHTTPSearcher(*baseURL, *timeout)
	if err != nil {
		exitf("%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout*time.Duration(max(1, len(cases))))
	defer cancel()
	report := evaluation.Evaluate(ctx, searcher, cases, *topK)
	printReport(report)
	if *output != "" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			exitf("encode report: %v", err)
		}
		if err := os.WriteFile(*output, append(encoded, '\n'), 0o600); err != nil {
			exitf("write report: %v", err)
		}
	}
	if hasCaseErrors(report) {
		os.Exit(1)
	}
}

func printReport(report evaluation.Report) {
	fmt.Printf("Retrieval eval: %d/%d passed (%.1f%%)\n", report.Passed, report.Total, report.PassRate*100)
	for _, result := range report.Cases {
		status := "MISS"
		if result.Hit {
			status = "HIT"
		}
		fmt.Printf("\n[%s] %s\n", status, result.ID)
		fmt.Printf("query: %s\n", result.Query)
		if result.RetrievalMode != "" {
			fmt.Printf("retrieval_mode: %s\n", result.RetrievalMode)
		}
		fmt.Printf("top_k_result_ids: %v\n", result.TopKResultIDs)
		fmt.Printf("matched_keywords: %v\n", result.MatchedKeywords)
		if result.BM25Score != nil {
			fmt.Printf("bm25_score: %.6g\n", *result.BM25Score)
		}
		if result.VectorScore != nil {
			fmt.Printf("vector_score: %.6g\n", *result.VectorScore)
		}
		if result.HybridScore != nil {
			fmt.Printf("hybrid_score: %.6g\n", *result.HybridScore)
		}
		if result.RRFScore != nil {
			fmt.Printf("rrf_score: %.6g\n", *result.RRFScore)
		}
		if result.RerankProvider != "" {
			fmt.Printf("rerank_provider: %s\n", result.RerankProvider)
		}
		if result.RerankScore != nil {
			fmt.Printf("rerank_score: %.6g\n", *result.RerankScore)
		}
		if result.RerankReason != "" {
			fmt.Printf("rerank_reason: %s\n", result.RerankReason)
		}
		if result.RerankFallbackReason != "" {
			fmt.Printf("rerank_fallback_reason: %s\n", result.RerankFallbackReason)
		}
		if result.EmptyRecall {
			fmt.Println("empty_recall: true")
		}
		if result.Error != "" {
			fmt.Printf("error: %s\n", result.Error)
		}
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "retrieval eval failed: "+format+"\n", args...)
	os.Exit(1)
}

func hasCaseErrors(report evaluation.Report) bool {
	for _, result := range report.Cases {
		if result.Error != "" {
			return true
		}
	}
	return false
}
