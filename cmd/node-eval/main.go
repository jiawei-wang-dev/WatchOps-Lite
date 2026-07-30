package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval/nodeeval"
)

func main() {
	datasetPath := flag.String(
		"dataset", "testdata/node_eval_cases.json", "node eval dataset",
	)
	jsonPath := flag.String(
		"json", "tmp/node_eval_report.json", "JSON report path",
	)
	markdownPath := flag.String(
		"markdown", "tmp/node_eval_report.md", "Markdown report path",
	)
	flag.Parse()
	file, err := os.Open(*datasetPath)
	if err != nil {
		exitf("open dataset: %v", err)
	}
	defer file.Close()
	dataset, err := nodeeval.LoadDataset(file)
	if err != nil {
		exitf("%v", err)
	}
	report := nodeeval.Evaluate(context.Background(), dataset)
	if err := nodeeval.WriteReports(report, *jsonPath, *markdownPath); err != nil {
		exitf("write reports: %v", err)
	}
	fmt.Printf(
		"node eval: %v/%v passed; reports: %s, %s\n",
		report.Summary["passed"], report.Summary["total"],
		*jsonPath, *markdownPath,
	)
	if failures := nodeeval.ThresholdFailures(report, os.Getenv); len(failures) > 0 {
		for _, failure := range failures {
			fmt.Fprintln(os.Stderr, failure)
		}
		os.Exit(1)
	}
}

func exitf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "node eval failed: "+format+"\n", arguments...)
	os.Exit(1)
}
