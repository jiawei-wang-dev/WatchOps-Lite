package eino

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const PromptVersionV1 = "watchops_agent_v1"

const systemPromptV1 = `You are WatchOps-Lite, a service reliability analysis assistant.

Evidence rules:
- Do not claim an observed fact unless it is supported by evidence returned by a tool.
- Use query_logs, query_metrics, query_traces, or search_knowledge when external evidence is needed.
- Use query_alerts and get_service_topology only as auxiliary on-call context for alert state and dependency context.
- Never invent evidence IDs. Reference only IDs present in tool results.
- Treat recommendations as proposed next steps, not observed facts.
- If evidence is missing or a tool fails, state that clearly in limitations.

Language rules:
- Respond in the same language as the user's latest message.
- If the latest message is Chinese, write all natural-language text values in Chinese.
- If the latest message is English, write all natural-language text values in English.
- Preserve tool names, evidence IDs, trace IDs, request IDs, metric names, service names, log IDs, and endpoint paths exactly.
- Keep the JSON field names below unchanged in every language.
- In Chinese responses, technical terms may be bilingual on first mention, for example 证据 Evidence, 工具调用 Tool Call, 局限性 Limitations, 熔断 Circuit Breaker, and 重试放大 Retry Amplification.

Your final response must be JSON only, with this shape:
{
  "conclusions": [{"text": "...", "evidence_ids": ["..."]}],
  "inferences": [{"text": "...", "evidence_ids": ["..."]}],
  "recommendations": [{"text": "...", "evidence_ids": ["..."]}],
  "limitations": [{"code": "...", "message": "...", "tool": "..."}]
}

Conclusions and inferences without valid evidence IDs will be discarded. Tool evidence and tool run summaries are attached by WatchOps-Lite after execution.`

const userPromptV1 = `Prompt version: {{.prompt_version}}

Session summary:
{{.session_summary}}

Recent messages:
{{.recent_messages}}

Confirmed long-term memories:
{{.confirmed_long_term_memories}}

Available diagnostic skills:
{{.diagnostic_skills}}

User profile context:
{{.user_profile_context}}

Pre-retrieved relevant knowledge:
{{.retrieved_knowledge}}

Pre-RAG metadata:
{{.pre_rag_metadata}}

Pre-RAG instructions:
- Treat the pre-retrieved chunks as initial context, not final proof.
- If logs, metrics, traces, or new clues suggest a different path, call search_knowledge again for second-pass RAG.
- Prefer citing chunk IDs or evidence IDs when knowledge supports a recommendation.
- If Pre-RAG is unavailable or insufficient, state that as a limitation instead of inventing context.

Current message:
{{.current_message}}

Time range:
from={{.time_from}}
to={{.time_to}}

Analyze the request. Call tools when evidence is needed, then return the required JSON object in the language of the current message.`

type PromptBuilder struct {
	version  string
	template *prompt.DefaultChatTemplate
}

type promptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func NewPromptBuilder(version string) (*PromptBuilder, error) {
	if version != PromptVersionV1 {
		return nil, fmt.Errorf("unsupported Agent prompt version %q", version)
	}
	return &PromptBuilder{
		version: version,
		template: prompt.FromMessages(
			schema.GoTemplate,
			schema.SystemMessage(systemPromptV1),
			schema.UserMessage(userPromptV1),
		),
	}, nil
}

func (b *PromptBuilder) Build(ctx context.Context, input AgentInput) ([]*schema.Message, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"agent.prompt.render",
		attribute.String("prompt_version", b.version),
		attribute.Int("recent_message_count", len(input.RecentMessages)),
	)
	defer span.End()

	summary, err := json.Marshal(input.SessionSummary)
	if err != nil {
		observability.MarkError(span, "Agent prompt rendering failed")
		return nil, fmt.Errorf("encode session summary: %w", err)
	}
	recent := make([]promptMessage, 0, len(input.RecentMessages))
	for _, message := range input.RecentMessages {
		recent = append(recent, promptMessage{
			Role:    string(message.Role),
			Content: message.Content,
		})
	}
	recentMessages, err := json.Marshal(recent)
	if err != nil {
		observability.MarkError(span, "Agent prompt rendering failed")
		return nil, fmt.Errorf("encode recent messages: %w", err)
	}
	longTermMemories, err := json.Marshal(input.ConfirmedLongTermMemories)
	if err != nil {
		observability.MarkError(span, "Agent prompt rendering failed")
		return nil, fmt.Errorf("encode long-term memories: %w", err)
	}
	diagnosticSkills, err := json.Marshal(input.DiagnosticSkills)
	if err != nil {
		observability.MarkError(span, "Agent prompt rendering failed")
		return nil, fmt.Errorf("encode diagnostic skills: %w", err)
	}
	userProfileContext, err := json.Marshal(input.UserProfileContext)
	if err != nil {
		observability.MarkError(span, "Agent prompt rendering failed")
		return nil, fmt.Errorf("encode user profile context: %w", err)
	}
	retrievedKnowledge, err := json.Marshal(input.RetrievedKnowledge)
	if err != nil {
		observability.MarkError(span, "Agent prompt rendering failed")
		return nil, fmt.Errorf("encode retrieved knowledge: %w", err)
	}
	preRAGMetadata, err := json.Marshal(input.PreRAGMetadata)
	if err != nil {
		observability.MarkError(span, "Agent prompt rendering failed")
		return nil, fmt.Errorf("encode pre-rag metadata: %w", err)
	}
	messages, err := b.template.Format(ctx, map[string]any{
		"prompt_version":               b.version,
		"session_summary":              string(summary),
		"recent_messages":              string(recentMessages),
		"confirmed_long_term_memories": string(longTermMemories),
		"diagnostic_skills":            string(diagnosticSkills),
		"user_profile_context":         string(userProfileContext),
		"retrieved_knowledge":          string(retrievedKnowledge),
		"pre_rag_metadata":             string(preRAGMetadata),
		"current_message":              input.CurrentMessage,
		"time_from":                    input.TimeContext.From,
		"time_to":                      input.TimeContext.To,
	})
	if err != nil {
		observability.MarkError(span, "Agent prompt rendering failed")
		return nil, err
	}
	return messages, nil
}
