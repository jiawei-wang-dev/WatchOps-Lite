package intenteval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	builder.WriteString("# WatchOps Intent Eval Report\n\n")
	fmt.Fprintf(&builder, "- Dataset: `%s`\n", report.DatasetVersion)
	fmt.Fprintf(&builder, "- Cases: %d\n", report.Total)
	fmt.Fprintf(&builder, "- Joint matches: %d\n", report.Passed)
	fmt.Fprintf(&builder, "- Duration: %d ms\n\n", report.DurationMS)
	builder.WriteString("## Metrics\n\n")
	fmt.Fprintf(&builder, "- Intent Accuracy: `%.4f`\n",
		report.Metrics.IntentAccuracy)
	fmt.Fprintf(&builder, "- Slot Field Accuracy: `%.4f`\n",
		report.Metrics.SlotFieldAccuracy)
	fmt.Fprintf(&builder, "- Joint Intent + Slot Exact Match: `%.4f`\n",
		report.Metrics.JointIntentSlotExactMatch)
	for _, field := range slotFields {
		fmt.Fprintf(&builder, "  - %s: `%.4f`\n",
			field,
			report.Metrics.SlotFieldAccuracyByField[field],
		)
	}
	builder.WriteString("\n## Failed Cases\n\n")
	failed := 0
	for _, current := range report.Cases {
		if current.Passed {
			continue
		}
		failed++
		fmt.Fprintf(&builder, "- `%s`: %s; intent `%s` → `%s`; slots `%s` → `%s`\n",
			current.ID,
			current.FailureReason,
			current.ExpectedIntent,
			current.ActualIntent,
			formatSlots(current.ExpectedSlots),
			formatSlots(current.ActualSlots),
		)
	}
	if failed == 0 {
		builder.WriteString("None.\n")
	}
	return builder.String()
}

func ThresholdFailures(report Report, getenv func(string) string) []string {
	checks := []struct {
		env    string
		actual float64
	}{
		{"WATCHOPS_INTENT_ACCURACY_MIN", report.Metrics.IntentAccuracy},
		{
			"WATCHOPS_SLOT_EXTRACTION_ACCURACY_MIN",
			report.Metrics.SlotFieldAccuracy,
		},
		{
			"WATCHOPS_JOINT_INTENT_SLOT_MIN",
			report.Metrics.JointIntentSlotExactMatch,
		},
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
		if check.actual < minimum {
			failures = append(failures, fmt.Sprintf(
				"%s: %.4f is below %.4f",
				check.env,
				check.actual,
				minimum,
			))
		}
	}
	return failures
}

func formatSlots(slots map[string]string) string {
	values := make([]string, 0, len(slotFields))
	for _, field := range slotFields {
		values = append(values, field+"="+slots[field])
	}
	return strings.Join(values, ",")
}
