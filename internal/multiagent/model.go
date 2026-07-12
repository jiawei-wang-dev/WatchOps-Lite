package multiagent

import (
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/diagnosis"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type AgentRole string

const (
	AgentRoleTriage    AgentRole = "triage"
	AgentRoleEvidence  AgentRole = "evidence"
	AgentRoleKnowledge AgentRole = "knowledge"
	AgentRoleSynthesis AgentRole = "synthesis"
)

type AgentStepStatus string

const (
	AgentStepPending   AgentStepStatus = "pending"
	AgentStepRunning   AgentStepStatus = "running"
	AgentStepCompleted AgentStepStatus = "completed"
	AgentStepDegraded  AgentStepStatus = "degraded"
	AgentStepFailed    AgentStepStatus = "failed"
	AgentStepSkipped   AgentStepStatus = "skipped"
)

type AgentStep struct {
	Role        AgentRole              `json:"role"`
	Name        string                 `json:"name"`
	Status      AgentStepStatus        `json:"status"`
	Input       string                 `json:"input,omitempty"`
	Output      string                 `json:"output,omitempty"`
	EvidenceIDs []string               `json:"evidence_ids,omitempty"`
	ToolRuns    []agenteino.ToolRun    `json:"tool_runs,omitempty"`
	Limitations []agenteino.Limitation `json:"limitations,omitempty"`
	StartedAt   time.Time              `json:"started_at,omitempty"`
	CompletedAt time.Time              `json:"completed_at,omitempty"`
	DurationMS  int64                  `json:"duration_ms,omitempty"`
	Metadata    map[string]any         `json:"metadata,omitempty"`
}

type TriagePlan struct {
	Service      string                  `json:"service"`
	IncidentType string                  `json:"incident_type"`
	EvidencePlan []string                `json:"evidence_plan"`
	Query        string                  `json:"query"`
	Summary      string                  `json:"summary"`
	TimeContext  common.TimeRange        `json:"time_context"`
	Language     string                  `json:"language"`
	Intent       intent.IntentResult     `json:"intent,omitempty"`
	AgentPlan    AgentPlan               `json:"agent_plan,omitempty"`
	Hypotheses   diagnosis.HypothesisSet `json:"hypotheses,omitempty"`
	RoleRAG      RoleRAGContext          `json:"role_rag,omitempty"`
	Limitations  []agenteino.Limitation  `json:"limitations,omitempty"`
	Metadata     map[string]any          `json:"metadata,omitempty"`
}

type RoleRAGContext struct {
	ChunksByRole     map[AgentRole][]retrievalknowledge.RetrievedKnowledge `json:"chunks_by_role,omitempty"`
	SynthesisSummary string                                                `json:"synthesis_summary,omitempty"`
	Metadata         map[string]any                                        `json:"metadata,omitempty"`
}

type AgentFinding struct {
	Role        AgentRole              `json:"role"`
	Summary     string                 `json:"summary"`
	Evidence    []common.EvidenceItem  `json:"evidence,omitempty"`
	EvidenceIDs []string               `json:"evidence_ids"`
	ToolRuns    []agenteino.ToolRun    `json:"tool_runs,omitempty"`
	Limitations []agenteino.Limitation `json:"limitations,omitempty"`
	Metadata    map[string]any         `json:"metadata,omitempty"`
}

type MergedFindings struct {
	Plan             TriagePlan             `json:"triage_plan"`
	EvidenceFinding  AgentFinding           `json:"evidence_finding"`
	KnowledgeFinding AgentFinding           `json:"knowledge_finding"`
	Evidence         []common.EvidenceItem  `json:"evidence"`
	EvidenceIDs      []string               `json:"evidence_ids"`
	ToolRuns         []agenteino.ToolRun    `json:"tool_runs"`
	Limitations      []agenteino.Limitation `json:"limitations"`
	Metadata         map[string]any         `json:"metadata,omitempty"`
}

type FinalDiagnosis struct {
	Language        string                `json:"language"`
	Summary         string                `json:"summary"`
	Incident        IncidentOverview      `json:"incident"`
	Findings        []FinalFinding        `json:"findings"`
	RootCause       RootCauseAssessment   `json:"root_cause_assessment"`
	Recommendations []FinalRecommendation `json:"recommendations"`
	Limitations     []string              `json:"limitations"`
	EvidenceRefs    []string              `json:"evidence_refs"`
	Metadata        map[string]any        `json:"metadata,omitempty"`
}

type IncidentOverview struct {
	Service      string `json:"service"`
	IncidentType string `json:"incident_type"`
	Severity     string `json:"severity"`
	Status       string `json:"status"`
}

type FinalFinding struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	EvidenceIDs []string `json:"evidence_ids"`
	Confidence  string   `json:"confidence"`
}

type RootCauseAssessment struct {
	Conclusion   string   `json:"conclusion"`
	Confidence   string   `json:"confidence"`
	EvidenceIDs  []string `json:"evidence_ids"`
	Alternatives []string `json:"alternatives"`
}

type FinalRecommendation struct {
	Priority     string `json:"priority"`
	Action       string `json:"action"`
	Reason       string `json:"reason"`
	Risk         string `json:"risk"`
	Verification string `json:"verification"`
}

type MultiAgentResult struct {
	Steps       []AgentStep           `json:"steps"`
	Evidence    []common.EvidenceItem `json:"evidence"`
	ToolRuns    []agenteino.ToolRun   `json:"tool_runs"`
	FinalAnswer agenteino.AgentOutput `json:"final_answer"`
	Metadata    map[string]any        `json:"metadata,omitempty"`
}

func RoleOrder() []AgentRole {
	return []AgentRole{
		AgentRoleTriage,
		AgentRoleEvidence,
		AgentRoleKnowledge,
		AgentRoleSynthesis,
	}
}
