package dto

type ChatRequest struct {
	SessionID   string         `json:"session_id" binding:"required"`
	UserID      string         `json:"user_id,omitempty"`
	Message     string         `json:"message" binding:"required"`
	TimeContext TimeContext    `json:"time_context" binding:"required"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type TimeContext struct {
	From string `json:"from" binding:"required"`
	To   string `json:"to" binding:"required"`
}

type ChatResponse struct {
	RequestID string         `json:"request_id"`
	SessionID string         `json:"session_id"`
	Answer    Answer         `json:"answer"`
	ToolRuns  []ToolRunDTO   `json:"tool_runs"`
	TraceID   string         `json:"trace_id"`
	Metadata  map[string]any `json:"metadata"`
}

type Answer struct {
	Conclusion      []ConclusionItem     `json:"conclusion"`
	Evidence        []EvidenceItemDTO    `json:"evidence"`
	Inferences      []InferenceItem      `json:"inferences"`
	Recommendations []RecommendationItem `json:"recommendations"`
	Limitations     []LimitationItem     `json:"limitations"`
}

type ConclusionItem struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type EvidenceItemDTO struct {
	ID         string         `json:"id"`
	SourceType string         `json:"source_type"`
	SourceName string         `json:"source_name"`
	TimeRange  *TimeContext   `json:"time_range,omitempty"`
	Content    string         `json:"content"`
	ResourceID string         `json:"resource_id,omitempty"`
	Score      *float64       `json:"score,omitempty"`
	Confidence *float64       `json:"confidence,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type InferenceItem struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type RecommendationItem struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type LimitationItem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Tool    string `json:"tool,omitempty"`
}

type ToolRunDTO struct {
	Tool          string `json:"tool"`
	Success       bool   `json:"success"`
	DurationMS    int64  `json:"duration_ms"`
	ErrorCode     string `json:"error_code,omitempty"`
	EvidenceCount int    `json:"evidence_count"`
	WarningCount  int    `json:"warning_count"`
}

type MultiAgentResponse struct {
	RequestID  string            `json:"request_id"`
	SessionID  string            `json:"session_id"`
	Mode       string            `json:"mode"`
	Answer     Answer            `json:"answer"`
	AgentSteps []AgentStepDTO    `json:"agent_steps"`
	Evidence   []EvidenceItemDTO `json:"evidence"`
	ToolRuns   []ToolRunDTO      `json:"tool_runs"`
	TraceID    string            `json:"trace_id"`
	Metadata   map[string]any    `json:"metadata"`
}

type AgentStepDTO struct {
	Role        string           `json:"role"`
	Name        string           `json:"name"`
	Status      string           `json:"status"`
	Input       string           `json:"input,omitempty"`
	Output      string           `json:"output,omitempty"`
	EvidenceIDs []string         `json:"evidence_ids"`
	ToolRuns    []ToolRunDTO     `json:"tool_runs"`
	Limitations []LimitationItem `json:"limitations"`
	StartedAt   string           `json:"started_at,omitempty"`
	CompletedAt string           `json:"completed_at,omitempty"`
	DurationMS  int64            `json:"duration_ms"`
	Metadata    map[string]any   `json:"metadata,omitempty"`
}

type ChatHistoryResponse struct {
	SessionID string               `json:"session_id"`
	Summary   ChatHistorySummary   `json:"summary"`
	Messages  []ChatHistoryMessage `json:"messages"`
	Limit     int                  `json:"limit"`
	Count     int                  `json:"count"`
}

type ChatHistorySummary struct {
	Content           string   `json:"content"`
	Version           int64    `json:"version"`
	UpdatedAt         string   `json:"updated_at,omitempty"`
	Goal              string   `json:"goal"`
	ConfirmedFacts    []string `json:"confirmed_facts"`
	OpenQuestions     []string `json:"open_questions"`
	AttemptedActions  []string `json:"attempted_actions"`
	ImportantEntities []string `json:"important_entities"`
}

type ChatHistoryMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	CreatedAt string         `json:"created_at,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ClearChatHistoryResponse struct {
	SessionID string `json:"session_id"`
	Cleared   bool   `json:"cleared"`
}

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Details   []any  `json:"details"`
}
