package intenteval

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

var slotFields = []string{"service", "time_range", "trace_id", "symptom"}

type Dataset struct {
	Version  string `json:"version"`
	FixedNow string `json:"fixed_now"`
	Cases    []Case `json:"cases"`
}

type Case struct {
	ID             string            `json:"id"`
	Message        string            `json:"message"`
	ExpectedIntent string            `json:"expected_intent"`
	ExpectedSlots  map[string]string `json:"expected_slots"`
	Notes          string            `json:"notes,omitempty"`
}

type CaseResult struct {
	ID             string            `json:"id"`
	Passed         bool              `json:"passed"`
	FailureReason  string            `json:"failure_reason,omitempty"`
	LatencyMS      float64           `json:"latency_ms"`
	ExpectedIntent string            `json:"expected_intent"`
	ActualIntent   string            `json:"actual_intent"`
	ExpectedSlots  map[string]string `json:"expected_slots"`
	ActualSlots    map[string]string `json:"actual_slots"`
	Confidence     float64           `json:"confidence"`
	Source         string            `json:"source"`
}

type Metrics struct {
	IntentAccuracy            float64                   `json:"intent_accuracy"`
	SlotFieldAccuracy         float64                   `json:"slot_field_accuracy"`
	SlotFieldAccuracyByField  map[string]float64        `json:"slot_field_accuracy_by_field"`
	JointIntentSlotExactMatch float64                   `json:"joint_intent_slot_exact_match"`
	AverageLatencyMS          float64                   `json:"average_latency_ms"`
	ConfusionMatrix           map[string]map[string]int `json:"intent_confusion_matrix"`
}

type Report struct {
	DatasetVersion string         `json:"dataset_version"`
	GeneratedAt    time.Time      `json:"generated_at"`
	DurationMS     int64          `json:"duration_ms"`
	Configuration  map[string]any `json:"configuration"`
	Total          int            `json:"total"`
	Passed         int            `json:"passed"`
	Failed         int            `json:"failed"`
	Metrics        Metrics        `json:"metrics"`
	Cases          []CaseResult   `json:"cases"`
}

func LoadDataset(reader io.Reader) (Dataset, error) {
	var dataset Dataset
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dataset); err != nil {
		return Dataset{}, fmt.Errorf("decode intent eval dataset: %w", err)
	}
	if strings.TrimSpace(dataset.Version) == "" {
		return Dataset{}, fmt.Errorf("dataset version is required")
	}
	if _, err := time.Parse(time.RFC3339, dataset.FixedNow); err != nil {
		return Dataset{}, fmt.Errorf("fixed_now must be RFC3339")
	}
	if len(dataset.Cases) == 0 {
		return Dataset{}, fmt.Errorf("intent eval dataset requires cases")
	}
	seen := map[string]struct{}{}
	for index := range dataset.Cases {
		current := &dataset.Cases[index]
		current.ID = strings.TrimSpace(current.ID)
		current.Message = strings.TrimSpace(current.Message)
		if current.ID == "" || current.Message == "" {
			return Dataset{}, fmt.Errorf("case[%d] requires id and message", index)
		}
		if _, duplicate := seen[current.ID]; duplicate {
			return Dataset{}, fmt.Errorf("duplicate intent eval case id %q", current.ID)
		}
		seen[current.ID] = struct{}{}
		if !validIntent(current.ExpectedIntent) {
			return Dataset{}, fmt.Errorf(
				"case %q has invalid expected_intent %q",
				current.ID,
				current.ExpectedIntent,
			)
		}
		if current.ExpectedSlots == nil {
			current.ExpectedSlots = map[string]string{}
		}
		for key := range current.ExpectedSlots {
			if !contains(slotFields, key) {
				return Dataset{}, fmt.Errorf(
					"case %q has unsupported expected slot %q",
					current.ID,
					key,
				)
			}
		}
	}
	return dataset, nil
}

func validIntent(value string) bool {
	switch intent.IntentType(value) {
	case intent.IntentIncidentTriage, intent.IntentMetricsQuery,
		intent.IntentLogsQuery, intent.IntentTraceAnalysis,
		intent.IntentKnowledgeQuery, intent.IntentStatusSummary,
		intent.IntentMitigation, intent.IntentGeneralChat:
		return true
	default:
		return false
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
