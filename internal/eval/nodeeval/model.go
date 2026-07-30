package nodeeval

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
)

type EvalStage string

const (
	EvalStageIntent   EvalStage = "intent"
	EvalStageSlot     EvalStage = "slot"
	EvalStageContext  EvalStage = "context"
	EvalStageRouting  EvalStage = "routing"
	EvalStageFallback EvalStage = "fallback"
)

type Dataset struct {
	Version  string         `json:"version"`
	FixedNow string         `json:"fixed_now"`
	Intent   []IntentCase   `json:"intent"`
	Slot     []SlotCase     `json:"slot"`
	Context  []ContextCase  `json:"context"`
	Routing  []RoutingCase  `json:"routing"`
	Fallback []FallbackCase `json:"fallback"`
}

type Focus struct {
	LastIntent      string            `json:"last_intent,omitempty"`
	KnownSlots      map[string]string `json:"known_slots,omitempty"`
	PendingQuestion string            `json:"pending_question,omitempty"`
	Candidates      []string          `json:"candidates,omitempty"`
	TurnStatus      string            `json:"turn_status,omitempty"`
}

type IntentCase struct {
	ID                    string  `json:"id"`
	Message               string  `json:"message"`
	Focus                 *Focus  `json:"session_focus,omitempty"`
	ExpectedIntent        string  `json:"expected_intent"`
	ExpectedService       string  `json:"expected_service,omitempty"`
	ExpectedSymptom       string  `json:"expected_symptom,omitempty"`
	ExpectedTraceID       string  `json:"expected_trace_id,omitempty"`
	ExpectedConfidenceMin float64 `json:"expected_confidence_min,omitempty"`
	ExpectedSource        string  `json:"expected_source,omitempty"`
	Notes                 string  `json:"notes,omitempty"`
}

type SlotCase struct {
	ID                       string            `json:"id"`
	Message                  string            `json:"message"`
	Intent                   string            `json:"intent,omitempty"`
	Focus                    *Focus            `json:"session_focus,omitempty"`
	ExpectedDecision         string            `json:"expected_decision"`
	ExpectedKnownSlots       map[string]string `json:"expected_known_slots,omitempty"`
	ExpectedMissingRequired  []string          `json:"expected_missing_required,omitempty"`
	ExpectedReasonCode       string            `json:"expected_reason_code,omitempty"`
	ExpectedQuestionContains string            `json:"expected_question_contains,omitempty"`
	ExpectedNoToolExecution  bool              `json:"expected_no_tool_execution"`
}

type ContextCase struct {
	ID    string        `json:"id"`
	Turns []ContextTurn `json:"turns"`
}

type ContextTurn struct {
	Message        string            `json:"message"`
	ExpectedStatus string            `json:"expected_status,omitempty"`
	ExpectedIntent string            `json:"expected_intent,omitempty"`
	ExpectedSlots  map[string]string `json:"expected_slots,omitempty"`
}

type RoutingCase struct {
	ID                            string   `json:"id"`
	Intent                        string   `json:"intent"`
	Confidence                    float64  `json:"confidence"`
	ExpectedSelectedAgents        []string `json:"expected_selected_agents"`
	ExpectedSkippedAgents         []string `json:"expected_skipped_agents,omitempty"`
	ExpectedDynamicRoutingEnabled bool     `json:"expected_dynamic_routing_enabled"`
}

type FallbackCase struct {
	ID                 string `json:"id"`
	Failure            string `json:"failure"`
	ExpectedFallback   bool   `json:"expected_fallback"`
	ExpectedSafeCode   string `json:"expected_safe_code"`
	ExpectedLimitation bool   `json:"expected_limitation"`
	ExpectedNoPanic    bool   `json:"expected_no_panic"`
}

type CaseResult struct {
	ID            string         `json:"id"`
	Passed        bool           `json:"passed"`
	FailureReason string         `json:"failure_reason,omitempty"`
	LatencyMS     float64        `json:"latency_ms"`
	Actual        map[string]any `json:"actual,omitempty"`
	Verification  string         `json:"verification"`
}

type StageReport struct {
	Stage    EvalStage      `json:"stage"`
	Total    int            `json:"total"`
	Passed   int            `json:"passed"`
	Failed   int            `json:"failed"`
	Verified int            `json:"verified"`
	Metrics  map[string]any `json:"metrics"`
	Cases    []CaseResult   `json:"cases"`
}

type Report struct {
	DatasetVersion string                    `json:"dataset_version"`
	GeneratedAt    time.Time                 `json:"generated_at"`
	DurationMS     int64                     `json:"duration_ms"`
	Configuration  map[string]any            `json:"configuration"`
	RealLLMUsed    bool                      `json:"real_llm_used"`
	Summary        map[string]any            `json:"summary"`
	Stages         map[EvalStage]StageReport `json:"stages"`
}

func LoadDataset(reader io.Reader) (Dataset, error) {
	var dataset Dataset
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dataset); err != nil {
		return Dataset{}, fmt.Errorf("decode node eval dataset: %w", err)
	}
	if strings.TrimSpace(dataset.Version) == "" {
		return Dataset{}, fmt.Errorf("dataset version is required")
	}
	if _, err := time.Parse(time.RFC3339, dataset.FixedNow); err != nil {
		return Dataset{}, fmt.Errorf("fixed_now must be RFC3339")
	}
	seen := map[string]struct{}{}
	check := func(stage EvalStage, id, message string) error {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%s case id is required", stage)
		}
		key := string(stage) + ":" + id
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate %s case id %q", stage, id)
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("%s case %q message/query is required", stage, id)
		}
		return nil
	}
	for _, current := range dataset.Intent {
		if err := check(EvalStageIntent, current.ID, current.Message); err != nil {
			return Dataset{}, err
		}
		if !validIntent(current.ExpectedIntent) {
			return Dataset{}, fmt.Errorf("intent case %q has invalid expected_intent", current.ID)
		}
	}
	for _, current := range dataset.Slot {
		if err := check(EvalStageSlot, current.ID, current.Message); err != nil {
			return Dataset{}, err
		}
		if current.ExpectedDecision != "proceed" &&
			current.ExpectedDecision != "clarify" &&
			current.ExpectedDecision != "fallback" {
			return Dataset{}, fmt.Errorf("slot case %q has invalid decision", current.ID)
		}
		if current.ExpectedDecision == "clarify" &&
			!current.ExpectedNoToolExecution {
			return Dataset{}, fmt.Errorf(
				"slot case %q must require no tool execution when clarifying",
				current.ID,
			)
		}
	}
	for _, current := range dataset.Context {
		if err := check(EvalStageContext, current.ID, "turns"); err != nil {
			return Dataset{}, err
		}
		if len(current.Turns) == 0 {
			return Dataset{}, fmt.Errorf("context case %q requires turns", current.ID)
		}
		for index, turn := range current.Turns {
			if strings.TrimSpace(turn.Message) == "" {
				return Dataset{}, fmt.Errorf(
					"context case %q turn[%d] message is required",
					current.ID,
					index,
				)
			}
			if strings.TrimSpace(turn.ExpectedStatus) == "" &&
				strings.TrimSpace(turn.ExpectedIntent) == "" &&
				len(turn.ExpectedSlots) == 0 {
				return Dataset{}, fmt.Errorf(
					"context case %q turn[%d] requires an expectation",
					current.ID,
					index,
				)
			}
		}
	}
	for _, current := range dataset.Routing {
		if err := check(EvalStageRouting, current.ID, current.Intent); err != nil {
			return Dataset{}, err
		}
		if !validIntent(current.Intent) {
			return Dataset{}, fmt.Errorf("routing case %q has invalid intent", current.ID)
		}
		if len(current.ExpectedSelectedAgents) == 0 {
			return Dataset{}, fmt.Errorf(
				"routing case %q requires expected_selected_agents",
				current.ID,
			)
		}
	}
	for _, current := range dataset.Fallback {
		if err := check(EvalStageFallback, current.ID, current.Failure); err != nil {
			return Dataset{}, err
		}
		if strings.TrimSpace(current.ExpectedSafeCode) == "" {
			return Dataset{}, fmt.Errorf(
				"fallback case %q requires expected_safe_code",
				current.ID,
			)
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
