package summary

import (
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
)

const summarySystemPrompt = `You summarize an ongoing service-reliability investigation.

Return one JSON object only with exactly these fields:
- content: concise narrative summary
- goal: current investigation goal
- confirmed_facts: array of evidence-supported facts
- open_questions: array of unresolved questions
- attempted_actions: array of tools or actions already attempted
- important_entities: array of service names, trace IDs, request IDs, evidence IDs, tool names, error codes, and time ranges

Rules:
- Never turn a user guess into a confirmed fact.
- Never turn assistant speculation into a confirmed fact unless the supplied context identifies supporting evidence.
- Preserve identifiers and time ranges exactly.
- Preserve unresolved questions.
- Keep the result concise.
- Return JSON only, without Markdown fences or commentary.`

type promptPayload struct {
	PromptVersion  string            `json:"prompt_version"`
	CurrentSummary session.Summary   `json:"current_summary"`
	Messages       []session.Message `json:"messages"`
}

func renderPrompt(
	promptVersion string,
	current session.Summary,
	messages []session.Message,
) ([]*schema.Message, error) {
	encoded, err := json.Marshal(promptPayload{
		PromptVersion:  promptVersion,
		CurrentSummary: current,
		Messages:       messages,
	})
	if err != nil {
		return nil, fmt.Errorf("encode summary prompt: %w", err)
	}
	return []*schema.Message{
		schema.SystemMessage(summarySystemPrompt),
		schema.UserMessage(string(encoded)),
	}, nil
}
