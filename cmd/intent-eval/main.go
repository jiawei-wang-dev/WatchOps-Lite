package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval/intenteval"
)

func main() {
	datasetPath := flag.String(
		"dataset", "testdata/intent_eval_cases.json", "intent eval dataset",
	)
	jsonPath := flag.String(
		"json", "tmp/intent_eval_report.json", "JSON report path",
	)
	markdownPath := flag.String(
		"markdown", "tmp/intent_eval_report.md", "Markdown report path",
	)
	flag.Parse()
	file, err := os.Open(*datasetPath)
	if err != nil {
		exitf("open dataset: %v", err)
	}
	defer file.Close()
	dataset, err := intenteval.LoadDataset(file)
	if err != nil {
		exitf("%v", err)
	}
	report := intenteval.Evaluate(context.Background(), dataset)
	if err := intenteval.WriteReports(
		report,
		*jsonPath,
		*markdownPath,
	); err != nil {
		exitf("write reports: %v", err)
	}
	fmt.Printf(
		"intent eval: cases=%d intent_accuracy=%.4f "+
			"slot_field_accuracy=%.4f joint_exact_match=%.4f\n",
		report.Total,
		report.Metrics.IntentAccuracy,
		report.Metrics.SlotFieldAccuracy,
		report.Metrics.JointIntentSlotExactMatch,
	)
	for _, current := range report.Cases {
		if !current.Passed {
			fmt.Printf("FAIL %s: %s\n", current.ID, current.FailureReason)
		}
	}
	fmt.Printf("reports: %s, %s\n", *jsonPath, *markdownPath)
	if failures := intenteval.ThresholdFailures(
		report,
		os.Getenv,
	); len(failures) > 0 {
		for _, failure := range failures {
			fmt.Fprintln(os.Stderr, failure)
		}
		os.Exit(1)
	}
}

func exitf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "intent eval failed: "+format+"\n", arguments...)
	os.Exit(1)
}
