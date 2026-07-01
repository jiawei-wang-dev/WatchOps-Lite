package summary

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
)

type modelStub struct {
	response *schema.Message
	err      error
	wait     bool
	input    []*schema.Message
}

func (m *modelStub) Generate(
	ctx context.Context,
	input []*schema.Message,
	_ ...model.Option,
) (*schema.Message, error) {
	m.input = input
	if m.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return m.response, m.err
}

func TestRenderPromptIncludesCurrentSummaryAndMessages(t *testing.T) {
	current := session.Summary{Content: "Existing checkout investigation"}
	messages := []session.Message{{
		Role:      session.RoleUser,
		Content:   "Is trace abc123 slow?",
		RequestID: "req-001",
	}}

	prompt, err := renderPrompt("session_summary_v1", current, messages)
	if err != nil {
		t.Fatalf("renderPrompt() error = %v", err)
	}
	if len(prompt) != 2 ||
		!strings.Contains(prompt[1].Content, current.Content) ||
		!strings.Contains(prompt[1].Content, messages[0].Content) ||
		!strings.Contains(prompt[0].Content, "Never turn a user guess into a confirmed fact") {
		t.Fatalf("prompt = %#v", prompt)
	}
}

func TestLLMSummarizerMapsValidJSON(t *testing.T) {
	chatModel := &modelStub{response: schema.AssistantMessage(`{
		"content":"Checkout errors are under investigation.",
		"goal":"Identify the checkout error-rate cause.",
		"confirmed_facts":["error rate is 6.2%"],
		"open_questions":["Is payment latency causal?"],
		"attempted_actions":["query_metrics"],
		"important_entities":["checkout","trace-001"]
	}`, nil)}
	summarizer, err := NewLLM(chatModel, NewDeterministic(), LLMConfig{
		PromptVersion: "session_summary_v1",
		Timeout:       time.Second,
	})
	if err != nil {
		t.Fatalf("NewLLM() error = %v", err)
	}

	result, err := summarizer.Summarize(
		context.Background(),
		session.Summary{Version: 3},
		[]session.Message{{Role: session.RoleUser, Content: "Why did checkout fail?"}},
	)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if result.Version != 3 ||
		result.Goal != "Identify the checkout error-rate cause." ||
		len(result.ConfirmedFacts) != 1 ||
		result.ImportantEntities[1] != "trace-001" {
		t.Fatalf("result = %#v", result)
	}
}

func TestLLMSummarizerFallsBackForMalformedJSON(t *testing.T) {
	summarizer, err := NewLLM(
		&modelStub{response: schema.AssistantMessage("not-json", nil)},
		NewDeterministic(),
		LLMConfig{PromptVersion: "session_summary_v1", Timeout: time.Second},
	)
	if err != nil {
		t.Fatalf("NewLLM() error = %v", err)
	}

	result, err := summarizer.Summarize(
		context.Background(),
		session.EmptySummary(),
		[]session.Message{{Role: session.RoleUser, Content: "Why is checkout slow?"}},
	)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if result.Goal != "Why is checkout slow?" ||
		!strings.Contains(result.Content, "Why is checkout slow?") {
		t.Fatalf("fallback result = %#v", result)
	}
}

func TestLLMSummarizerFallsBackAfterTimeout(t *testing.T) {
	summarizer, err := NewLLM(
		&modelStub{wait: true},
		NewDeterministic(),
		LLMConfig{PromptVersion: "session_summary_v1", Timeout: 5 * time.Millisecond},
	)
	if err != nil {
		t.Fatalf("NewLLM() error = %v", err)
	}

	result, err := summarizer.Summarize(
		context.Background(),
		session.EmptySummary(),
		[]session.Message{{Role: session.RoleUser, Content: "Check trace trace-001."}},
	)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if result.Goal != "Check trace trace-001." {
		t.Fatalf("fallback result = %#v", result)
	}
}

func TestLLMSummarizerFallsBackForModelError(t *testing.T) {
	summarizer, err := NewLLM(
		&modelStub{err: errors.New("model unavailable")},
		NewDeterministic(),
		LLMConfig{PromptVersion: "session_summary_v1", Timeout: time.Second},
	)
	if err != nil {
		t.Fatalf("NewLLM() error = %v", err)
	}
	if _, err := summarizer.Summarize(
		context.Background(),
		session.EmptySummary(),
		[]session.Message{{Role: session.RoleUser, Content: "Investigate checkout."}},
	); err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
}
