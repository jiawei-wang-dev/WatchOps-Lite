package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type Dataset struct {
	Version   string     `json:"version"`
	FixedNow  string     `json:"fixed_now"`
	Scenarios []Scenario `json:"scenarios"`
	Corpus    []Scenario `json:"-"`
}

type Scenario struct {
	CaseID                  string                       `json:"case_id"`
	Category                string                       `json:"category"`
	Input                   string                       `json:"input"`
	InitialContext          map[string]any               `json:"initial_context,omitempty"`
	ExpectedIntent          string                       `json:"expected_intent"`
	ExpectedSlots           map[string]string            `json:"expected_slots"`
	RequiredTools           []string                     `json:"required_tools"`
	AcceptableTools         []string                     `json:"acceptable_tools"`
	ForbiddenTools          []string                     `json:"forbidden_tools"`
	ExpectedArguments       map[string]string            `json:"expected_arguments"`
	QuerySemanticTerms      []string                     `json:"query_semantic_terms,omitempty"`
	RequiredEvidenceTypes   []string                     `json:"required_evidence_types"`
	AcceptableEvidenceTypes []string                     `json:"acceptable_evidence_types,omitempty"`
	ExpectedDecision        string                       `json:"expected_decision"`
	ExpectedStatus          string                       `json:"expected_status,omitempty"`
	RelevantDocumentIDs     []string                     `json:"relevant_document_ids"`
	RetrievalExpectation    string                       `json:"retrieval_expectation,omitempty"`
	Fixtures                map[string][]EvidenceFixture `json:"fixtures"`
	ToolBehaviors           map[string]string            `json:"tool_behaviors,omitempty"`
	RepeatedToolProbe       *RepeatedToolProbe           `json:"repeated_tool_probe,omitempty"`
}

type RepeatedToolProbe struct {
	Tool      string `json:"tool"`
	Arguments string `json:"arguments"`
	Attempts  int    `json:"attempts"`
}

type EvidenceFixture struct {
	ID         string `json:"id"`
	SourceType string `json:"source_type"`
	Content    string `json:"content"`
}

type IntentMetrics struct {
	Cases                 int     `json:"cases"`
	Accuracy              float64 `json:"accuracy"`
	SlotFieldAccuracy     float64 `json:"slot_field_accuracy"`
	JointExactMatch       float64 `json:"joint_exact_match"`
	ClarificationAccuracy float64 `json:"clarification_accuracy"`
}

type RetrievalMetrics struct {
	Mode              string  `json:"mode"`
	QueryMode         string  `json:"query_mode"`
	Cases             int     `json:"cases"`
	RecallAt1         float64 `json:"recall_at_1"`
	RecallAt3         float64 `json:"recall_at_3"`
	RecallAt5         float64 `json:"recall_at_5"`
	HitAt1            float64 `json:"hit_at_1"`
	HitAt3            float64 `json:"hit_at_3"`
	HitAt5            float64 `json:"hit_at_5"`
	MRR               float64 `json:"mrr"`
	AverageCandidates float64 `json:"average_candidate_count"`
	AverageLatencyMS  float64 `json:"average_latency_ms"`
}

type MultiQueryMetrics struct {
	Cases                          int      `json:"cases"`
	SingleQueryCases               int      `json:"single_query_cases"`
	MultiQueryCases                int      `json:"multi_query_cases"`
	MultiQueryTriggerRate          float64  `json:"multi_query_trigger_rate"`
	MultiQueryCorrectTriggerRate   float64  `json:"multi_query_correct_trigger_rate"`
	IncorrectMultiQueryTriggerRate float64  `json:"incorrect_multi_query_trigger_rate"`
	NeutralMultiQueryTriggerRate   float64  `json:"neutral_multi_query_trigger_rate"`
	SingleQueryRecallAt5           float64  `json:"single_query_recall_at_5"`
	SingleQueryMRR                 float64  `json:"single_query_mrr"`
	MultiQueryRecallAt5            float64  `json:"multi_query_recall_at_5"`
	MultiQueryMRR                  float64  `json:"multi_query_mrr"`
	ConditionalRecallAt5           float64  `json:"conditional_recall_at_5"`
	ConditionalMRR                 float64  `json:"conditional_mrr"`
	TriggeredCaseIDs               []string `json:"triggered_case_ids"`
	ImprovedCaseIDs                []string `json:"improved_case_ids"`
	WorsenedCaseIDs                []string `json:"worsened_case_ids"`
	NeutralCaseIDs                 []string `json:"neutral_case_ids"`
}

type ToolEvidenceMetrics struct {
	Cases                           int                `json:"cases"`
	ToolSelectionAccuracy           float64            `json:"tool_selection_accuracy"`
	ToolArgumentAccuracy            float64            `json:"tool_argument_accuracy"`
	ArgumentFieldAccuracy           map[string]float64 `json:"argument_field_accuracy"`
	RequiredToolRecall              float64            `json:"required_tool_recall"`
	ForbiddenToolViolationRate      float64            `json:"forbidden_tool_violation_rate"`
	UnnecessaryToolCallRate         float64            `json:"unnecessary_tool_call_rate"`
	ToolPathValidity                float64            `json:"tool_path_validity"`
	ForbiddenToolCallCount          int                `json:"forbidden_tool_call_count"`
	EvidenceCitationValidity        float64            `json:"evidence_citation_validity"`
	EvidenceAllowlistViolationCount int                `json:"evidence_allowlist_violation_count"`
	EvidenceCoverage                float64            `json:"evidence_coverage"`
	EvidenceAllowlistViolationRate  float64            `json:"evidence_allowlist_violation_rate"`
}

type AgentMetrics struct {
	Cases                    int      `json:"cases"`
	Completed                int      `json:"completed"`
	Clarified                int      `json:"clarified"`
	Failed                   int      `json:"failed"`
	Timeout                  int      `json:"timeout"`
	AverageToolCalls         float64  `json:"average_tool_calls"`
	AverageAgentSteps        *float64 `json:"average_agent_steps"`
	P50LatencyMS             float64  `json:"p50_latency_ms"`
	P95LatencyMS             float64  `json:"p95_latency_ms"`
	TaskSuccessRate          float64  `json:"task_success_rate"`
	DecisionAccuracy         float64  `json:"decision_accuracy"`
	RequiredEvidenceCoverage float64  `json:"required_evidence_coverage"`
	UnsafeActionRate         float64  `json:"unsafe_action_rate"`
	FailureRate              float64  `json:"failure_rate"`
	TimeoutRate              float64  `json:"timeout_rate"`
	RepeatedToolCallStops    int      `json:"repeated_tool_call_stops"`
}

type AgentCaseResult struct {
	CaseID                   string            `json:"case_id"`
	Category                 string            `json:"category"`
	Input                    string            `json:"input"`
	InitialContext           map[string]any    `json:"initial_context,omitempty"`
	ExpectedIntent           string            `json:"expected_intent"`
	ExpectedSlots            map[string]string `json:"expected_slots"`
	RequiredTools            []string          `json:"required_tools"`
	AcceptableTools          []string          `json:"acceptable_tools"`
	ForbiddenTools           []string          `json:"forbidden_tools"`
	RequiredEvidenceTypes    []string          `json:"required_evidence_types"`
	ExpectedDecision         string            `json:"expected_decision"`
	ActualTrace              []string          `json:"actual_trace"`
	ActualTools              []string          `json:"actual_tools"`
	ActualEvidence           []string          `json:"actual_evidence"`
	FinalStatus              string            `json:"final_status"`
	TaskSuccess              bool              `json:"task_success"`
	ObservedStatus           string            `json:"observed_status"`
	DecisionCorrect          bool              `json:"decision_correct"`
	RequiredEvidenceCoverage float64           `json:"required_evidence_coverage"`
	UnsafeAction             bool              `json:"unsafe_action"`
	RepeatedToolCallStop     bool              `json:"repeated_tool_call_stop"`
	FailureReason            string            `json:"failure_reason,omitempty"`
	LatencyMS                float64           `json:"latency_ms"`
}

type ChallengeDataset struct {
	Version  string          `json:"version"`
	FixedNow string          `json:"fixed_now"`
	Cases    []ChallengeCase `json:"cases"`
}

type ChallengeCase struct {
	ID               string            `json:"id"`
	ConversationID   string            `json:"conversation_id,omitempty"`
	TurnIndex        int               `json:"turn_index,omitempty"`
	Message          string            `json:"message"`
	InitialFocus     map[string]any    `json:"initial_focus,omitempty"`
	ExpectedIntent   string            `json:"expected_intent"`
	ExpectedSlots    map[string]string `json:"expected_slots"`
	ExpectedDecision string            `json:"expected_decision"`
	RuleSufficient   bool              `json:"rule_sufficient"`
}

type ChallengeMetrics struct {
	Cases                      int     `json:"cases"`
	IntentAccuracy             float64 `json:"intent_accuracy"`
	SlotCompleteness           float64 `json:"slot_completeness"`
	ClarificationPrecision     float64 `json:"clarification_precision"`
	ClarificationRecall        float64 `json:"clarification_recall"`
	OverClarificationRate      float64 `json:"over_clarification_rate"`
	UnderClarificationRate     float64 `json:"under_clarification_rate"`
	LLMEscalationRate          float64 `json:"llm_escalation_rate"`
	IncorrectLLMEscalationRate float64 `json:"incorrect_llm_escalation_rate"`
}

type BadCaseBaseline struct {
	CaseID         string `json:"case_id"`
	Stage          string `json:"stage"`
	RootCause      string `json:"root_cause"`
	FixType        string `json:"fix_type"`
	BeforeBehavior string `json:"before_behavior"`
}

type BadCaseFix struct {
	CaseID         string `json:"case_id"`
	Stage          string `json:"stage"`
	RootCause      string `json:"root_cause"`
	FixType        string `json:"fix_type"`
	BeforeBehavior string `json:"before_behavior"`
	AfterBehavior  string `json:"after_behavior"`
	Fixed          bool   `json:"fixed"`
}

type BadCaseComparison struct {
	BeforeRecords     int          `json:"before_records"`
	BeforeUnique      int          `json:"before_unique_bad_cases"`
	AfterRecords      int          `json:"after_records"`
	AfterUnique       int          `json:"after_unique_bad_cases"`
	Fixed             int          `json:"fixed"`
	StillFailing      int          `json:"still_failing"`
	NewRegression     int          `json:"new_regression"`
	ChallengeFindings int          `json:"challenge_findings"`
	Cases             []BadCaseFix `json:"cases"`
}

type RunOptions struct {
	Challenge            ChallengeDataset
	Baseline             []BadCaseBaseline
	OptimizationBaseline OptimizationBaseline
	FinalBadCaseBaseline []FinalOptimizationBadCaseBaseline
}

type OptimizationIntentSnapshot struct {
	LLMEscalationRate         float64 `json:"llm_escalation_rate"`
	IncorrectEscalationRate   float64 `json:"incorrect_escalation_rate"`
	ChallengeBehaviorAccuracy float64 `json:"challenge_behavior_accuracy"`
	FixedRegressionAccuracy   float64 `json:"fixed_regression_accuracy"`
}

type OptimizationRetrievalSnapshot struct {
	SingleQueryRecallAt5 float64 `json:"single_query_recall_at_5"`
	SingleQueryMRR       float64 `json:"single_query_mrr"`
	MultiQueryRecallAt5  float64 `json:"multi_query_recall_at_5"`
	MultiQueryMRR        float64 `json:"multi_query_mrr"`
	BadCases             int     `json:"bad_cases"`
}

type OptimizationAgentSnapshot struct {
	TaskSuccessRate  float64 `json:"task_success_rate"`
	DecisionAccuracy float64 `json:"decision_accuracy"`
	UnsafeActionRate float64 `json:"unsafe_action_rate"`
	Timeout          int     `json:"timeout"`
	RepeatedStops    int     `json:"repeated_tool_call_stops"`
}

type OptimizationBaseline struct {
	CapturedAt string                        `json:"captured_at"`
	Intent     OptimizationIntentSnapshot    `json:"intent"`
	Retrieval  OptimizationRetrievalSnapshot `json:"retrieval"`
	AgentE2E   OptimizationAgentSnapshot     `json:"agent_e2e"`
}

type OptimizationComparison struct {
	Before OptimizationBaseline `json:"before"`
	After  OptimizationBaseline `json:"after"`
}

type FinalOptimizationBadCaseBaseline struct {
	CaseID    string `json:"case_id"`
	Stage     string `json:"stage"`
	Input     string `json:"input"`
	Expected  any    `json:"expected"`
	Actual    any    `json:"actual"`
	RootCause string `json:"root_cause"`
	FixType   string `json:"fix_type"`
}

type FinalOptimizationBadCaseResult struct {
	FinalOptimizationBadCaseBaseline
	AfterBehavior string `json:"after_behavior"`
	Fixed         bool   `json:"fixed"`
}

type FinalOptimizationBadCaseComparison struct {
	Before int                              `json:"before"`
	After  int                              `json:"after"`
	Fixed  int                              `json:"fixed"`
	Cases  []FinalOptimizationBadCaseResult `json:"cases"`
}

type RuleFirstBenchmark struct {
	Mode               string  `json:"mode"`
	Cases              int     `json:"cases"`
	RuleDirectCount    int     `json:"rule_direct_count"`
	LLMEscalationCount int     `json:"llm_escalation_count"`
	LLMCallRate        float64 `json:"llm_call_rate"`
	FallbackCount      int     `json:"fallback_count"`
	IntentAccuracy     float64 `json:"intent_accuracy"`
}

type ClarificationBenchmark struct {
	Cases             int `json:"cases"`
	RAGCalls          int `json:"rag_calls"`
	AgentCalls        int `json:"agent_calls"`
	ToolCalls         int `json:"tool_calls"`
	AvoidedRAGCalls   int `json:"avoided_rag_calls"`
	AvoidedAgentCalls int `json:"avoided_agent_calls"`
	AvoidedToolCalls  int `json:"avoided_tool_calls"`
}

type ContextMeasurement struct {
	MessageCount            int      `json:"message_count"`
	ContextChars            int      `json:"context_chars"`
	ContextBytes            int      `json:"context_bytes"`
	RetainedImportantFields []string `json:"retained_important_fields"`
	RedactedFields          []string `json:"redacted_fields"`
	FocusSizeBytes          int      `json:"focus_size_bytes"`
}

type ContextBenchmark struct {
	RawHistory ContextMeasurement `json:"raw_history"`
	Governed   ContextMeasurement `json:"recent_messages_summary_focus"`
}

type Benchmarks struct {
	RuleFirstBefore RuleFirstBenchmark     `json:"rule_first_before"`
	RuleFirstAfter  RuleFirstBenchmark     `json:"rule_first_after"`
	Clarification   ClarificationBenchmark `json:"clarification_early_exit"`
	Context         ContextBenchmark       `json:"context_governance"`
	Retrieval       []RetrievalMetrics     `json:"retrieval"`
}

type Report struct {
	Version                string                             `json:"version"`
	GeneratedAt            time.Time                          `json:"generated_at"`
	Mode                   string                             `json:"mode"`
	ExternalLLM            bool                               `json:"external_llm"`
	Intent                 IntentMetrics                      `json:"intent"`
	Challenge              ChallengeMetrics                   `json:"intent_challenge"`
	Retrieval              []RetrievalMetrics                 `json:"retrieval"`
	MultiQuery             MultiQueryMetrics                  `json:"multi_query_decision"`
	ToolEvidence           ToolEvidenceMetrics                `json:"tool_evidence"`
	AgentE2E               AgentMetrics                       `json:"agent_e2e"`
	AgentCases             []AgentCaseResult                  `json:"agent_cases"`
	Benchmarks             Benchmarks                         `json:"benchmarks"`
	BadCases               []BadCase                          `json:"bad_cases"`
	BadCaseComparison      BadCaseComparison                  `json:"bad_case_comparison"`
	Optimization           OptimizationComparison             `json:"optimization_before_after"`
	FinalBadCaseComparison FinalOptimizationBadCaseComparison `json:"final_optimization_bad_cases"`
	AgentSkipReason        string                             `json:"agent_skip_reason,omitempty"`
}

func LoadOptimizationBaseline(reader io.Reader) (OptimizationBaseline, error) {
	var baseline OptimizationBaseline
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&baseline); err != nil {
		return OptimizationBaseline{}, fmt.Errorf("decode optimization baseline: %w", err)
	}
	if strings.TrimSpace(baseline.CapturedAt) == "" {
		return OptimizationBaseline{}, fmt.Errorf("optimization baseline requires captured_at")
	}
	return baseline, nil
}

func LoadFinalOptimizationBadCaseBaseline(reader io.Reader) ([]FinalOptimizationBadCaseBaseline, error) {
	var baseline []FinalOptimizationBadCaseBaseline
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&baseline); err != nil {
		return nil, fmt.Errorf("decode final optimization bad case baseline: %w", err)
	}
	if len(baseline) == 0 {
		return nil, fmt.Errorf("final optimization bad case baseline is empty")
	}
	return baseline, nil
}

func LoadChallengeDataset(reader io.Reader) (ChallengeDataset, error) {
	var dataset ChallengeDataset
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dataset); err != nil {
		return ChallengeDataset{}, fmt.Errorf("decode intent challenge dataset: %w", err)
	}
	if dataset.Version == "" || len(dataset.Cases) == 0 {
		return ChallengeDataset{}, fmt.Errorf("intent challenge dataset requires version and cases")
	}
	if _, err := time.Parse(time.RFC3339, dataset.FixedNow); err != nil {
		return ChallengeDataset{}, fmt.Errorf("challenge fixed_now must be RFC3339")
	}
	return dataset, nil
}

func LoadBadCaseBaseline(reader io.Reader) ([]BadCaseBaseline, error) {
	var baseline []BadCaseBaseline
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&baseline); err != nil {
		return nil, fmt.Errorf("decode bad case baseline: %w", err)
	}
	return baseline, nil
}

func LoadDataset(reader io.Reader) (Dataset, error) {
	var dataset Dataset
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dataset); err != nil {
		return Dataset{}, fmt.Errorf("decode oncall eval dataset: %w", err)
	}
	if strings.TrimSpace(dataset.Version) == "" || len(dataset.Scenarios) == 0 {
		return Dataset{}, fmt.Errorf("oncall eval dataset requires version and scenarios")
	}
	if _, err := time.Parse(time.RFC3339, dataset.FixedNow); err != nil {
		return Dataset{}, fmt.Errorf("fixed_now must be RFC3339")
	}
	seen := map[string]struct{}{}
	for index, scenario := range dataset.Scenarios {
		if strings.TrimSpace(scenario.CaseID) == "" || strings.TrimSpace(scenario.Input) == "" {
			return Dataset{}, fmt.Errorf("scenario[%d] requires case_id and input", index)
		}
		if _, ok := seen[scenario.CaseID]; ok {
			return Dataset{}, fmt.Errorf("duplicate scenario case_id %q", scenario.CaseID)
		}
		seen[scenario.CaseID] = struct{}{}
	}
	return dataset, nil
}
