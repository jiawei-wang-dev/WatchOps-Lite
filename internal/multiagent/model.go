package multiagent

import (
	"time"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
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
	Service      string                 `json:"service"`
	IncidentType string                 `json:"incident_type"`
	EvidencePlan []string               `json:"evidence_plan"`
	Query        string                 `json:"query"`
	Summary      string                 `json:"summary"`
	TimeContext  common.TimeRange       `json:"time_context"`
	Language     string                 `json:"language"`
	Limitations  []agenteino.Limitation `json:"limitations,omitempty"`
	Metadata     map[string]any         `json:"metadata,omitempty"`
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
