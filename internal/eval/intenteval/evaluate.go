package intenteval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

func Evaluate(ctx context.Context, dataset Dataset) Report {
	started := time.Now()
	now, _ := time.Parse(time.RFC3339, dataset.FixedNow)
	report := Report{
		DatasetVersion: dataset.Version,
		GeneratedAt:    time.Now().UTC(),
		Configuration: map[string]any{
			"mode":      "local_rule_recognizer",
			"fixed_now": dataset.FixedNow,
			"network":   false,
			"real_llm":  false,
		},
		Total: len(dataset.Cases),
		Cases: make([]CaseResult, 0, len(dataset.Cases)),
	}
	recognizer := intent.NewRuleBasedRecognizer()
	intentMatches, jointMatches, slotMatches, slotComparisons := 0, 0, 0, 0
	fieldMatches := map[string]int{}
	confusion := map[string]map[string]int{}
	var totalLatency float64

	for _, current := range dataset.Cases {
		caseStarted := time.Now()
		result, err := recognizer.Recognize(ctx, intent.RecognitionInput{
			Message: current.Message,
			Now:     now,
		})
		latency := durationMS(caseStarted)
		totalLatency += latency
		actualIntent := string(result.Intent)
		if confusion[current.ExpectedIntent] == nil {
			confusion[current.ExpectedIntent] = map[string]int{}
		}
		confusion[current.ExpectedIntent][actualIntent]++

		actualSlots := slotsFromResult(result)
		expectedSlots := normalizedSlots(current.ExpectedSlots)
		intentOK := err == nil && actualIntent == current.ExpectedIntent
		if intentOK {
			intentMatches++
		}
		mismatches := make([]string, 0, len(slotFields)+1)
		if err != nil {
			mismatches = append(mismatches, "recognizer_error")
		} else if !intentOK {
			mismatches = append(mismatches, "intent")
		}
		slotsOK := true
		for _, field := range slotFields {
			slotComparisons++
			if actualSlots[field] == expectedSlots[field] {
				slotMatches++
				fieldMatches[field]++
				continue
			}
			slotsOK = false
			mismatches = append(mismatches, field)
		}
		jointOK := intentOK && slotsOK
		if jointOK {
			jointMatches++
			report.Passed++
		}
		report.Cases = append(report.Cases, CaseResult{
			ID:             current.ID,
			Passed:         jointOK,
			FailureReason:  mismatchReason(mismatches),
			LatencyMS:      latency,
			ExpectedIntent: current.ExpectedIntent,
			ActualIntent:   actualIntent,
			ExpectedSlots:  expectedSlots,
			ActualSlots:    actualSlots,
			Confidence:     result.Confidence,
			Source:         result.Source,
		})
	}

	fieldAccuracy := make(map[string]float64, len(slotFields))
	for _, field := range slotFields {
		fieldAccuracy[field] = ratio(fieldMatches[field], report.Total)
	}
	report.Failed = report.Total - report.Passed
	report.DurationMS = time.Since(started).Milliseconds()
	report.Metrics = Metrics{
		IntentAccuracy:            ratio(intentMatches, report.Total),
		SlotFieldAccuracy:         ratio(slotMatches, slotComparisons),
		SlotFieldAccuracyByField:  fieldAccuracy,
		JointIntentSlotExactMatch: ratio(jointMatches, report.Total),
		AverageLatencyMS:          average(totalLatency, report.Total),
		ConfusionMatrix:           confusion,
	}
	return report
}

func slotsFromResult(result intent.IntentResult) map[string]string {
	timeRange := ""
	if result.TimeRange != nil {
		switch {
		case strings.TrimSpace(result.TimeRange.Relative) != "":
			timeRange = strings.TrimSpace(result.TimeRange.Relative)
		case strings.TrimSpace(result.TimeRange.From) != "" ||
			strings.TrimSpace(result.TimeRange.To) != "":
			timeRange = strings.TrimSpace(result.TimeRange.From) +
				"/" + strings.TrimSpace(result.TimeRange.To)
		}
	}
	return map[string]string{
		"service":    strings.TrimSpace(result.Service),
		"time_range": timeRange,
		"trace_id":   strings.TrimSpace(result.TraceID),
		"symptom":    strings.TrimSpace(result.Symptom),
	}
}

func normalizedSlots(source map[string]string) map[string]string {
	result := make(map[string]string, len(slotFields))
	for _, field := range slotFields {
		result[field] = strings.TrimSpace(source[field])
	}
	return result
}

func mismatchReason(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	return fmt.Sprintf("mismatched fields: %s", strings.Join(fields, ", "))
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func average(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func durationMS(started time.Time) float64 {
	return float64(time.Since(started).Nanoseconds()) / 1e6
}
