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
- Never invent evidence IDs. Reference only IDs present in tool results.
- Treat recommendations as proposed next steps, not observed facts.
- If evidence is missing or a tool fails, state that clearly in limitations.

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

Diagnostic skill cards:
{{.diagnostic_skills}}

Preloaded knowledge:
{{.retrieved_knowledge}}

Current message:
{{.current_message}}

Time range:
from={{.time_from}}
to={{.time_to}}

Analyze the request. Call tools when evidence is needed, then return the required JSON object.`

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
	retrievedKnowledge, err := json.Marshal(input.RetrievedKnowledge)
	if err != nil {
		observability.MarkError(span, "Agent prompt rendering failed")
		return nil, fmt.Errorf("encode retrieved knowledge: %w", err)
	}
	messages, err := b.template.Format(ctx, map[string]any{
		"prompt_version":               b.version,
		"session_summary":              string(summary),
		"recent_messages":              string(recentMessages),
		"confirmed_long_term_memories": string(longTermMemories),
		"diagnostic_skills":            string(diagnosticSkills),
		"retrieved_knowledge":          string(retrievedKnowledge),
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
