package dto

type ChatRequest struct {
	SessionID   string      `json:"session_id" binding:"required"`
	Message     string      `json:"message" binding:"required"`
	TimeContext TimeContext `json:"time_context" binding:"required"`
}

type TimeContext struct {
	From string `json:"from" binding:"required"`
	To   string `json:"to" binding:"required"`
}

type ChatResponse struct {
	RequestID string       `json:"request_id"`
	SessionID string       `json:"session_id"`
	Answer    Answer       `json:"answer"`
	ToolRuns  []ToolRunDTO `json:"tool_runs"`
	TraceID   string       `json:"trace_id"`
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

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Details   []any  `json:"details"`
}
