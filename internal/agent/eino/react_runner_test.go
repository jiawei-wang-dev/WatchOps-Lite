package eino

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type scriptedToolCallingModel struct {
	responses  []*schema.Message
	err        error
	callCount  int
	boundTools []*schema.ToolInfo
}

func (m *scriptedToolCallingModel) Generate(
	context.Context,
	[]*schema.Message,
	...model.Option,
) (*schema.Message, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.callCount >= len(m.responses) {
		return nil, errors.New("no scripted model response")
	}
	response := m.responses[m.callCount]
	m.callCount++
	return response, nil
}

func (m *scriptedToolCallingModel) Stream(
	context.Context,
	[]*schema.Message,
	...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream is not supported by test model")
}

func (m *scriptedToolCallingModel) WithTools(
	tools []*schema.ToolInfo,
) (model.ToolCallingChatModel, error) {
	m.boundTools = tools
	return m, nil
}

func TestReActRunnerCallsEinoToolAndMapsEvidence(t *testing.T) {
	tools, err := BuildMockTools()
	if err != nil {
		t.Fatalf("BuildMockTools() error = %v", err)
	}
	chatModel := &scriptedToolCallingModel{responses: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID:   "call_metrics_1",
			Type: "function",
			Function: schema.FunctionCall{
				Name: "query_metrics",
				Arguments: `{
					"service":"checkout",
					"metric_name":"http_server_error_rate",
					"time_range":{
						"from":"2026-06-30T00:00:00Z",
						"to":"2026-06-30T00:20:00Z"
					}
				}`,
			},
		}}),
		schema.AssistantMessage(`{
			"conclusions":[{
				"text":"Checkout error rate is elevated.",
				"evidence_ids":["metric-evidence-001"]
			}],
			"inferences":[],
			"recommendations":[{
				"text":"Inspect upstream saturation.",
				"evidence_ids":["metric-evidence-001"]
			}],
			"limitations":[]
		}`, nil),
	}}
	runner, err := NewReActRunner(context.Background(), chatModel, tools, ReActRunnerConfig{
		MaxIterations: 3,
		Timeout:       time.Second,
		PromptVersion: PromptVersionV1,
		ModelName:     "test-model",
	})
	if err != nil {
		t.Fatalf("NewReActRunner() error = %v", err)
	}

	output, err := runner.Run(context.Background(), validAgentInput())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(chatModel.boundTools) != 4 || chatModel.callCount != 2 {
		t.Fatalf("bound tools=%d model calls=%d", len(chatModel.boundTools), chatModel.callCount)
	}
	if len(output.Evidence) != 1 ||
		output.Evidence[0].ID != "metric-evidence-001" ||
		len(output.ToolRuns) != 1 ||
		output.ToolRuns[0].Tool != "query_metrics" ||
		len(output.Conclusions) != 1 {
		t.Fatalf("output = %#v", output)
	}
	if output.Metadata["agent_mode"] != "eino_react" ||
		output.Metadata["output_parse_success"] != true {
		t.Fatalf("metadata = %#v", output.Metadata)
	}
}

func TestReActRunnerReturnsSafeLimitationForMalformedOutput(t *testing.T) {
	tools, _ := BuildMockTools()
	chatModel := &scriptedToolCallingModel{
		responses: []*schema.Message{schema.AssistantMessage("not-json", nil)},
	}
	runner, err := NewReActRunner(context.Background(), chatModel, tools, ReActRunnerConfig{
		MaxIterations: 2,
		Timeout:       time.Second,
		PromptVersion: PromptVersionV1,
		ModelName:     "test-model",
	})
	if err != nil {
		t.Fatalf("NewReActRunner() error = %v", err)
	}

	output, err := runner.Run(context.Background(), validAgentInput())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !hasLimitation(output.Limitations, "AGENT_OUTPUT_PARSE_FAILED") {
		t.Fatalf("limitations = %#v", output.Limitations)
	}
	if output.Metadata["output_parse_success"] != false {
		t.Fatalf("metadata = %#v", output.Metadata)
	}
}

func TestFallbackRunnerUsesDeterministicRunnerOnModelFailure(t *testing.T) {
	tools, _ := BuildMockTools()
	primary := &runnerStub{err: errors.New("model unavailable")}
	fallback := NewDeterministicRunner(tools)
	runner := NewFallbackRunner(primary, fallback)

	output, err := runner.Run(context.Background(), validAgentInput())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if output.Metadata["fallback_used"] != true ||
		output.Metadata["fallback_reason"] != "llm_unavailable" ||
		!hasLimitation(output.Limitations, "AGENT_LLM_FALLBACK") {
		t.Fatalf("output = %#v", output)
	}
}

func TestFallbackRunnerUsesDeterministicRunnerAfterPreparedFailure(t *testing.T) {
	tools, _ := BuildMockTools()
	primary := &preparedRunnerStub{err: errors.New("model unavailable")}
	runner := NewFallbackRunner(primary, NewDeterministicRunner(tools))
	input := validAgentInput()

	messages, err := runner.RenderPrompt(context.Background(), input)
	if err != nil {
		t.Fatalf("RenderPrompt() error = %v", err)
	}
	output, err := runner.RunPrepared(context.Background(), input, messages)
	if err != nil {
		t.Fatalf("RunPrepared() error = %v", err)
	}
	if !primary.preparedCalled ||
		output.Metadata["fallback_used"] != true ||
		!hasLimitation(output.Limitations, "AGENT_LLM_FALLBACK") {
		t.Fatalf("primary=%#v output=%#v", primary, output)
	}
}

func TestPromptBuilderIncludesAgentContext(t *testing.T) {
	builder, err := NewPromptBuilder(PromptVersionV1)
	if err != nil {
		t.Fatalf("NewPromptBuilder() error = %v", err)
	}
	input := validAgentInput()
	input.SessionSummary.Content = "Earlier checkout investigation"
	input.RecentMessages = []session.Message{{
		Role:    session.RoleUser,
		Content: "The previous alert mentioned timeouts.",
	}}
	input.ConfirmedLongTermMemories = []string{"Checkout is owned by the payments team."}
	input.DiagnosticSkills = []string{"metric_inspection: inspect service signals"}
	input.RetrievedKnowledge = []string{"No knowledge is preloaded; use search_knowledge."}

	messages, err := builder.Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	combined := messages[0].Content + messages[1].Content
	for _, expected := range []string{
		"service reliability analysis assistant",
		"Earlier checkout investigation",
		"The previous alert mentioned timeouts.",
		"Checkout is owned by the payments team.",
		"Available diagnostic skills:",
		"metric_inspection: inspect service signals",
		"No knowledge is preloaded; use search_knowledge.",
		input.CurrentMessage,
		input.TimeContext.From,
		input.TimeContext.To,
		PromptVersionV1,
	} {
		if !strings.Contains(combined, expected) {
			t.Fatalf("prompt is missing %q: %s", expected, combined)
		}
	}
}

func TestStructuredParserRejectsInventedEvidenceIDs(t *testing.T) {
	output := parseAgentOutput(
		`{
			"conclusions":[{"text":"Unsupported root cause.","evidence_ids":["invented-id"]}],
			"inferences":[],
			"recommendations":[],
			"limitations":[]
		}`,
		[]common.EvidenceItem{{ID: "real-id"}},
		[]ToolRun{},
		[]Limitation{},
		map[string]any{},
	)

	if len(output.Conclusions) != 0 ||
		!hasLimitation(output.Limitations, "EVIDENCE_REFERENCE_INVALID") {
		t.Fatalf("output = %#v", output)
	}
}

type runnerStub struct {
	output AgentOutput
	err    error
}

type preparedRunnerStub struct {
	err            error
	preparedCalled bool
}

func (r *preparedRunnerStub) Run(context.Context, AgentInput) (AgentOutput, error) {
	return AgentOutput{}, r.err
}

func (r *preparedRunnerStub) RenderPrompt(
	context.Context,
	AgentInput,
) ([]*schema.Message, error) {
	return []*schema.Message{schema.UserMessage("prepared")}, nil
}

func (r *preparedRunnerStub) RunPrepared(
	context.Context,
	AgentInput,
	[]*schema.Message,
) (AgentOutput, error) {
	r.preparedCalled = true
	return AgentOutput{}, r.err
}

func (r *runnerStub) Run(context.Context, AgentInput) (AgentOutput, error) {
	return r.output, r.err
}

func validAgentInput() AgentInput {
	return AgentInput{
		SessionSummary: session.EmptySummary(),
		RecentMessages: []session.Message{},
		CurrentMessage: "Why did checkout error rate increase?",
		TimeContext: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
	}
}

func hasLimitation(limitations []Limitation, code string) bool {
	for _, limitation := range limitations {
		if limitation.Code == code {
			return true
		}
	}
	return false
}
