# ADR 0008: Eino ReAct Agent

- Status: Accepted
- Date: 2026-07-01

## Context

The deterministic runner was introduced before an LLM so WatchOps-Lite could validate its Chat contract, tool schemas, evidence model, Redis context, retrieval, feedback, and tracing without nondeterministic dependencies. Its keyword routing is transparent and useful for tests, but it cannot reason about varied reliability questions or choose tools dynamically.

The next runtime must support real tool calling without replacing the established `AgentRunner` boundary or creating a second tool framework.

## Decision

Introduce an optional Eino ReAct runner using:

- Eino `ToolCallingChatModel`
- Eino `PromptTemplate`
- Eino ReAct Graph and `ToolsNode`
- the existing Eino `InvokableTool` values
- Eino `MessageFuture` for executed model/tool messages

The first provider adapter uses Eino's official OpenAI-compatible model component. API keys are read from a configured environment-variable name and never stored in the JSON config.

The deterministic runner remains:

- the default when LLM execution is disabled
- the startup fallback for incomplete configuration or model initialization failure
- the request-time fallback when a model call fails
- the stable test runner

## Prompt and Output Contract

Prompt version `watchops_agent_v1` receives the rolling session summary, recent messages, current message, and time range. It instructs the model to use tools for external facts, distinguish limitations, and return JSON containing conclusions, inferences, recommendations, and limitations.

WatchOps-Lite does not accept model-produced evidence as authoritative. Eino executes tools, and WatchOps-Lite collects the actual normalized `ToolResult` messages. The final parser:

1. parses the final JSON
2. attaches evidence and tool runs only from executed tools
3. removes conclusion/inference evidence IDs not present in those results
4. reports invalid references as `EVIDENCE_REFERENCE_INVALID`
5. reports malformed output as `AGENT_OUTPUT_PARSE_FAILED`

This keeps the existing `AgentOutput` schema while placing the evidence boundary outside the model.

## Tool Failure Policy

A small adapter converts structured tool errors into safe tool-result messages so the ReAct loop can observe a failure and continue. It does not register tools or replace Eino's ToolsNode. Raw backend and model errors are not returned to the API.

## Bounds and Observability

The Agent has a configured request timeout and maximum iteration count, mapped to Eino's graph-step bound. OpenTelemetry spans cover:

- `agent.eino.run`
- `agent.prompt.render`
- `agent.llm.call`
- `agent.tool_call`
- `agent.output.parse`
- `agent.fallback`

Span attributes include mode, model, prompt version, tool names/count, parse status, and fallback status—not prompts, user messages, model output, or credentials.

## Consequences

- Chat gains dynamic tool selection without changing its service or HTTP contract.
- Eino remains responsible for ReAct orchestration and tool calling.
- Deterministic behavior remains available for resilience and tests.
- OpenAI-compatible providers must support chat-completion tool calling.
- Structured output quality still depends on the model, but unsafe output is constrained by parsing and evidence allowlisting.

## Deferred Work

- multi-agent and supervisor workflows
- plan-and-execute agents
- LLM-based session summarization
- embedding/hybrid retrieval and reranking
- automatic eval runner and LLM judge
- advanced token, cost, and repeated-call budgets
