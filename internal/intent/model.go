package intent

import (
	"sort"
	"strings"
)

type IntentType string

const (
	IntentIncidentTriage IntentType = "incident_triage"
	IntentMetricsQuery   IntentType = "metrics_query"
	IntentLogsQuery      IntentType = "logs_query"
	IntentTraceAnalysis  IntentType = "trace_analysis"
	IntentKnowledgeQuery IntentType = "knowledge_query"
	IntentStatusSummary  IntentType = "status_summary"
	IntentMitigation     IntentType = "mitigation_advice"
	IntentGeneralChat    IntentType = "general_chat"
)

type ToolName string

const (
	ToolQueryMetrics    ToolName = "query_metrics"
	ToolQueryLogs       ToolName = "query_logs"
	ToolQueryTraces     ToolName = "query_traces"
	ToolSearchKnowledge ToolName = "search_knowledge"
)

type AgentRole string

const (
	RoleTriage    AgentRole = "triage"
	RoleEvidence  AgentRole = "evidence"
	RoleKnowledge AgentRole = "knowledge"
	RoleSynthesis AgentRole = "synthesis"
)

type IntentResult struct {
	Intent          IntentType         `json:"intent"`
	Confidence      float64            `json:"confidence"`
	Reason          string             `json:"reason,omitempty"`
	Service         string             `json:"service,omitempty"`
	TimeRange       *TimeRangeHint     `json:"time_range,omitempty"`
	TraceID         string             `json:"trace_id,omitempty"`
	Operation       string             `json:"operation,omitempty"`
	Symptom         string             `json:"symptom,omitempty"`
	Severity        string             `json:"severity,omitempty"`
	Keywords        []string           `json:"keywords,omitempty"`
	SuggestedTools  []ToolName         `json:"suggested_tools,omitempty"`
	SuggestedAgents []AgentRole        `json:"suggested_agents,omitempty"`
	SkillHints      []SkillHint        `json:"skill_hints,omitempty"`
	RAGHints        RAGHints           `json:"rag_hints,omitempty"`
	Source          string             `json:"source"`
	Limitations     []IntentLimitation `json:"limitations,omitempty"`
	Metadata        map[string]any     `json:"metadata,omitempty"`
}

type TimeRangeHint struct {
	Relative string `json:"relative,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
}

type SkillHint struct {
	ToolName ToolName `json:"tool_name"`
	Priority int      `json:"priority"`
	Reason   string   `json:"reason,omitempty"`
}

type RAGHints struct {
	QueryBoosts             []string `json:"query_boosts,omitempty"`
	Categories              []string `json:"categories,omitempty"`
	Tags                    []string `json:"tags,omitempty"`
	PreferRunbooks          bool     `json:"prefer_runbooks,omitempty"`
	PreferIncidents         bool     `json:"prefer_incidents,omitempty"`
	PreferObservabilityDocs bool     `json:"prefer_observability_docs,omitempty"`
	TopKOverride            int      `json:"top_k_override,omitempty"`
}

type IntentLimitation struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Normalize(result IntentResult) IntentResult {
	result.Intent = normalizeIntent(result.Intent)
	result.Confidence = clampConfidence(result.Confidence)
	result.Service = strings.TrimSpace(result.Service)
	result.TraceID = strings.ToLower(strings.TrimSpace(result.TraceID))
	result.Operation = strings.TrimSpace(result.Operation)
	result.Symptom = strings.TrimSpace(result.Symptom)
	result.Severity = strings.TrimSpace(result.Severity)
	result.Reason = strings.TrimSpace(result.Reason)
	result.Source = strings.TrimSpace(result.Source)
	if result.Source == "" {
		result.Source = "fallback"
	}
	result.Keywords = dedupeStrings(result.Keywords)
	result.RAGHints.QueryBoosts = dedupeStrings(result.RAGHints.QueryBoosts)
	result.RAGHints.Categories = dedupeStrings(result.RAGHints.Categories)
	result.RAGHints.Tags = dedupeStrings(result.RAGHints.Tags)
	result.SuggestedTools = defaultTools(result.Intent, result.SuggestedTools, result.TraceID)
	result.SuggestedAgents = defaultAgents(result.Intent, result.SuggestedAgents)
	result.SkillHints = normalizeSkillHints(result.SkillHints, result.SuggestedTools)
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["intent_type"] = string(result.Intent)
	result.Metadata["intent_source"] = result.Source
	return result
}

func SafeDefault(message string, limitations ...IntentLimitation) IntentResult {
	return Normalize(IntentResult{
		Intent:      IntentGeneralChat,
		Confidence:  0.5,
		Reason:      "safe default intent",
		Keywords:    keywordCandidates(message),
		Source:      "fallback",
		Limitations: limitations,
		Metadata:    map[string]any{"fallback_used": true},
	})
}

func AddLimitation(result IntentResult, code string, message string) IntentResult {
	if strings.TrimSpace(code) == "" {
		return result
	}
	result.Limitations = append(result.Limitations, IntentLimitation{
		Code:    code,
		Message: strings.TrimSpace(message),
	})
	return result
}

func defaultTools(intentType IntentType, suggested []ToolName, traceID string) []ToolName {
	tools := append([]ToolName{}, suggested...)
	switch intentType {
	case IntentIncidentTriage:
		tools = append(tools, ToolQueryMetrics, ToolQueryLogs, ToolSearchKnowledge)
	case IntentMetricsQuery:
		tools = append(tools, ToolQueryMetrics)
	case IntentLogsQuery:
		tools = append(tools, ToolQueryLogs)
	case IntentTraceAnalysis:
		tools = append(tools, ToolQueryTraces)
		if traceID != "" {
			tools = append([]ToolName{ToolQueryTraces}, tools...)
		}
	case IntentKnowledgeQuery, IntentMitigation:
		tools = append(tools, ToolSearchKnowledge)
	}
	return dedupeTools(tools)
}

func defaultAgents(intentType IntentType, suggested []AgentRole) []AgentRole {
	roles := append([]AgentRole{}, suggested...)
	switch intentType {
	case IntentIncidentTriage:
		roles = append(roles, RoleTriage, RoleEvidence, RoleKnowledge, RoleSynthesis)
	case IntentTraceAnalysis:
		roles = append(roles, RoleEvidence, RoleTriage, RoleSynthesis)
	case IntentKnowledgeQuery, IntentMitigation:
		roles = append(roles, RoleKnowledge, RoleSynthesis)
	case IntentMetricsQuery, IntentLogsQuery:
		roles = append(roles, RoleEvidence, RoleSynthesis)
	case IntentGeneralChat, IntentStatusSummary:
		roles = append(roles, RoleSynthesis)
	}
	return dedupeAgents(roles)
}

func normalizeIntent(value IntentType) IntentType {
	switch value {
	case IntentIncidentTriage,
		IntentMetricsQuery,
		IntentLogsQuery,
		IntentTraceAnalysis,
		IntentKnowledgeQuery,
		IntentStatusSummary,
		IntentMitigation,
		IntentGeneralChat:
		return value
	default:
		return IntentGeneralChat
	}
}

func clampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	if value == 0 {
		return 0.5
	}
	return value
}

func normalizeSkillHints(hints []SkillHint, tools []ToolName) []SkillHint {
	result := make([]SkillHint, 0, len(hints)+len(tools))
	seen := map[ToolName]struct{}{}
	for _, hint := range hints {
		if hint.ToolName == "" {
			continue
		}
		if hint.Priority <= 0 {
			hint.Priority = 50
		}
		result = append(result, hint)
		seen[hint.ToolName] = struct{}{}
	}
	for index, tool := range tools {
		if _, exists := seen[tool]; exists {
			continue
		}
		result = append(result, SkillHint{
			ToolName: tool,
			Priority: 100 - index,
			Reason:   "suggested by recognized intent",
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Priority > result[j].Priority
	})
	return result
}

func dedupeTools(values []ToolName) []ToolName {
	result := make([]ToolName, 0, len(values))
	seen := map[ToolName]struct{}{}
	for _, value := range values {
		if !validTool(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func dedupeAgents(values []AgentRole) []AgentRole {
	result := make([]AgentRole, 0, len(values))
	seen := map[AgentRole]struct{}{}
	for _, value := range values {
		if !validAgent(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func dedupeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func validTool(value ToolName) bool {
	switch value {
	case ToolQueryMetrics, ToolQueryLogs, ToolQueryTraces, ToolSearchKnowledge:
		return true
	default:
		return false
	}
}

func validAgent(value AgentRole) bool {
	switch value {
	case RoleTriage, RoleEvidence, RoleKnowledge, RoleSynthesis:
		return true
	default:
		return false
	}
}
