package nodeeval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func WriteReports(report Report, jsonPath, markdownPath string) error {
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	return os.WriteFile(markdownPath, []byte(Markdown(report)), 0o600)
}

func Markdown(report Report) string {
	var builder strings.Builder
	builder.WriteString("# WatchOps Node Eval Report\n\n")
	fmt.Fprintf(&builder, "- Dataset: `%s`\n", report.DatasetVersion)
	fmt.Fprintf(&builder, "- Mode: local deterministic (real LLM: `%t`)\n", report.RealLLMUsed)
	fmt.Fprintf(&builder, "- Duration: %d ms\n\n", report.DurationMS)
	builder.WriteString("| Stage | Matched | Cases | Executed verification |\n")
	builder.WriteString("|---|---:|---:|---:|\n")
	stages := []EvalStage{
		EvalStageIntent, EvalStageSlot, EvalStageContext,
		EvalStageRouting, EvalStageRetrieval, EvalStageFallback,
	}
	for _, name := range stages {
		stage := report.Stages[name]
		fmt.Fprintf(&builder, "| %s | %d | %d | %d |\n",
			name, stage.Passed, stage.Total, stage.Verified)
	}
	builder.WriteString("\n## Metrics\n\n")
	for _, name := range stages {
		stage := report.Stages[name]
		fmt.Fprintf(&builder, "### %s\n\n", name)
		keys := make([]string, 0, len(stage.Metrics))
		for key := range stage.Metrics {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			encoded, _ := json.Marshal(stage.Metrics[key])
			fmt.Fprintf(&builder, "- `%s`: `%s`\n", key, encoded)
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Failed Cases\n\n")
	failed := 0
	for _, name := range stages {
		for _, current := range report.Stages[name].Cases {
			if current.Passed {
				continue
			}
			failed++
			fmt.Fprintf(&builder, "- `%s/%s`: %s\n",
				name, current.ID, current.FailureReason)
		}
	}
	if failed == 0 {
		builder.WriteString("None.\n")
	}
	return builder.String()
}

func ThresholdFailures(report Report, getenv func(string) string) []string {
	checks := []struct {
		env    string
		stage  EvalStage
		metric string
	}{
		{"WATCHOPS_INTENT_ACCURACY_MIN", EvalStageIntent, "accuracy"},
		{"WATCHOPS_ROUTING_EXACT_MATCH_MIN", EvalStageRouting, "exact_match"},
		{"WATCHOPS_RETRIEVAL_HIT_RATE_MIN", EvalStageRetrieval, "hit_rate_at_k"},
		{"WATCHOPS_FALLBACK_PASS_RATE_MIN", EvalStageFallback, "contract_pass_rate"},
	}
	failures := []string{}
	for _, check := range checks {
		value := strings.TrimSpace(getenv(check.env))
		if value == "" {
			continue
		}
		minimum, err := strconv.ParseFloat(value, 64)
		if err != nil {
			failures = append(failures, check.env+" is not a number")
			continue
		}
		actual, ok := report.Stages[check.stage].Metrics[check.metric].(float64)
		if !ok || actual < minimum {
			failures = append(failures, fmt.Sprintf(
				"%s: %.4f is below %.4f", check.env, actual, minimum,
			))
		}
	}
	return failures
}
