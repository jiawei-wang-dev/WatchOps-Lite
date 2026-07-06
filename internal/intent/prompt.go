package intent

import (
	"encoding/json"
	"strings"
)

const intentSystemPrompt = `You are an intent recognizer for an on-call diagnostic agent.
Return strict JSON only.

Available intents:
- incident_triage
- metrics_query
- logs_query
- trace_analysis
- knowledge_query
- status_summary
- mitigation_advice
- general_chat

Available tools:
- query_metrics
- query_logs
- query_traces
- search_knowledge

Available agents:
- triage
- evidence
- knowledge
- synthesis

Do not call tools. Do not invent tool names or agent roles.`

func buildIntentPrompt(input RecognitionInput) string {
	payload := map[string]any{
		"message":          input.Message,
		"session_id":       input.SessionID,
		"user_id":          input.UserID,
		"now":              input.Now.Format("2006-01-02T15:04:05Z07:00"),
		"recent_messages":  boundedMessages(input.RecentMessages),
		"available_tools":  input.AvailableTools,
		"available_skills": input.AvailableSkills,
		"instructions": []string{
			"Output JSON with fields: intent, confidence, service, time_range, trace_id, operation, symptom, severity, keywords, suggested_tools, suggested_agents, rag_hints, reason.",
			"Use only the available tools and agents listed above.",
			"If uncertain, choose general_chat or incident_triage with lower confidence.",
		},
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func boundedMessages(messages []MessageView) []map[string]string {
	if len(messages) > 6 {
		messages = messages[len(messages)-6:]
	}
	result := make([]map[string]string, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if len([]rune(content)) > 300 {
			content = string([]rune(content)[:300])
		}
		result = append(result, map[string]string{
			"role":    message.Role,
			"content": content,
		})
	}
	return result
}
