package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval/harness"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval/intenteval"
)

func main() {
	var (
		datasetPath              = flag.String("dataset", "testdata/oncall_eval_scenarios.json", "on-call fixture dataset")
		intentPath               = flag.String("intent-dataset", "testdata/intent_eval_cases.json", "intent dataset")
		challengePath            = flag.String("intent-challenge", "testdata/intent_challenge_cases.json", "hard intent challenge dataset")
		baselinePath             = flag.String("bad-case-baseline", "testdata/eval_bad_case_baseline.json", "before-fix bad case baseline")
		optimizationBaselinePath = flag.String("optimization-baseline", "testdata/eval_final_optimization_baseline.json", "final optimization before benchmark")
		finalBadCaseBaselinePath = flag.String("final-bad-case-baseline", "testdata/eval_final_bad_case_baseline.json", "final optimization bad case baseline")
		jsonPath                 = flag.String("json", "data/eval/report.json", "JSON report path")
		markdownPath             = flag.String("markdown", "data/eval/report.md", "Markdown report path")
		badCasePath              = flag.String("bad-cases", "data/eval/bad_cases.json", "bad case JSON path")
		replayPath               = flag.String("replay", "", "optional bad case JSON to replay")
	)
	flag.Parse()

	datasetFile, err := os.Open(*datasetPath)
	if err != nil {
		exitf("open dataset: %v", err)
	}
	defer datasetFile.Close()
	dataset, err := harness.LoadDataset(datasetFile)
	if err != nil {
		exitf("%v", err)
	}
	finalBadCaseBaselineFile, err := os.Open(*finalBadCaseBaselinePath)
	if err != nil {
		exitf("open final bad case baseline: %v", err)
	}
	finalBadCaseBaseline, err := harness.LoadFinalOptimizationBadCaseBaseline(finalBadCaseBaselineFile)
	finalBadCaseBaselineFile.Close()
	if err != nil {
		exitf("%v", err)
	}
	optimizationBaselineFile, err := os.Open(*optimizationBaselinePath)
	if err != nil {
		exitf("open optimization baseline: %v", err)
	}
	optimizationBaseline, err := harness.LoadOptimizationBaseline(optimizationBaselineFile)
	optimizationBaselineFile.Close()
	if err != nil {
		exitf("%v", err)
	}

	intentFile, err := os.Open(*intentPath)
	if err != nil {
		exitf("open intent dataset: %v", err)
	}
	defer intentFile.Close()
	intentDataset, err := intenteval.LoadDataset(intentFile)
	if err != nil {
		exitf("%v", err)
	}

	if *replayPath != "" {
		replayFile, openErr := os.Open(*replayPath)
		if openErr != nil {
			exitf("open replay file: %v", openErr)
		}
		badCases, loadErr := harness.LoadBadCases(replayFile)
		replayFile.Close()
		if loadErr != nil {
			exitf("%v", loadErr)
		}
		ids := harness.ReplayCaseIDs(badCases)
		dataset.Corpus = append([]harness.Scenario{}, dataset.Scenarios...)
		dataset.Scenarios = filterScenarios(dataset.Scenarios, ids)
		intentDataset.Cases = filterIntentCases(intentDataset.Cases, ids)
		fmt.Printf("replaying %d stable bad-case IDs\n", len(ids))
	}
	challengeFile, err := os.Open(*challengePath)
	if err != nil {
		exitf("open intent challenge dataset: %v", err)
	}
	challengeDataset, err := harness.LoadChallengeDataset(challengeFile)
	challengeFile.Close()
	if err != nil {
		exitf("%v", err)
	}
	baselineFile, err := os.Open(*baselinePath)
	if err != nil {
		exitf("open bad case baseline: %v", err)
	}
	baseline, err := harness.LoadBadCaseBaseline(baselineFile)
	baselineFile.Close()
	if err != nil {
		exitf("%v", err)
	}
	if *replayPath != "" {
		replayFile, openErr := os.Open(*replayPath)
		if openErr != nil {
			exitf("open replay file: %v", openErr)
		}
		badCases, loadErr := harness.LoadBadCases(replayFile)
		replayFile.Close()
		if loadErr != nil {
			exitf("%v", loadErr)
		}
		challengeDataset.Cases = filterChallengeCases(challengeDataset.Cases, harness.ReplayCaseIDs(badCases))
	}

	report, err := harness.Run(context.Background(), dataset, intentDataset, harness.RunOptions{
		Challenge: challengeDataset, Baseline: baseline,
		OptimizationBaseline: optimizationBaseline,
		FinalBadCaseBaseline: finalBadCaseBaseline,
	})
	if err != nil {
		exitf("run evaluation: %v", err)
	}
	if err := harness.WriteReports(report, *jsonPath, *markdownPath, *badCasePath); err != nil {
		exitf("write reports: %v", err)
	}
	fmt.Printf(
		"evaluation complete: intent=%d challenge=%d retrieval_modes=%d agent_cases=%d bad_cases=%d external_llm=false\n",
		report.Intent.Cases, report.Challenge.Cases, len(report.Retrieval), report.AgentE2E.Cases, len(report.BadCases),
	)
}

func filterChallengeCases(values []harness.ChallengeCase, ids map[string]struct{}) []harness.ChallengeCase {
	result := []harness.ChallengeCase{}
	for _, item := range values {
		if _, ok := ids[item.ID]; ok {
			result = append(result, item)
		}
	}
	return result
}

func filterScenarios(values []harness.Scenario, ids map[string]struct{}) []harness.Scenario {
	result := []harness.Scenario{}
	for _, item := range values {
		if _, ok := ids[item.CaseID]; ok {
			result = append(result, item)
		}
	}
	return result
}

func filterIntentCases(values []intenteval.Case, ids map[string]struct{}) []intenteval.Case {
	result := []intenteval.Case{}
	for _, item := range values {
		if _, ok := ids[item.ID]; ok {
			result = append(result, item)
		}
	}
	return result
}

func exitf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "evaluation failed: "+format+"\n", arguments...)
	os.Exit(1)
}
