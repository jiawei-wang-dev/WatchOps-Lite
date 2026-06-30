# ADR 0003: Deterministic Chat and Agent Skeleton

- Status: Accepted
- Date: 2026-06-30

## Context

Phase 2 established typed WatchOps tool contracts and exposed deterministic mock tools through Eino `InvokableTool` values. Before adding a real model, ReAct loop, memory, or retrieval backend, the project needs to validate its public Chat API, package boundaries, answer schema, tool summaries, and evidence flow.

Using a deterministic skeleton makes those contracts testable without network services, credentials, or nondeterministic model output.

## Decision

Phase 3 adds:

- `POST /api/v1/chat` through Gin
- Transport-specific Chat DTOs
- An application-level Chat service
- An Eino-based deterministic Agent runner
- Simple, explicit message-to-tool routing
- Evidence-aware answer mapping
- Tool run summaries and visible limitations

The runner is Eino-based because it selects and executes the Eino `InvokableTool` values created in Phase 2. It does not implement a custom Tool Registry.

The runner is not a full ReAct Agent. It does not use Eino Graph, `ChatModel`, `PromptTemplate`, or an LLM in this phase.

## Request Flow

```text
Gin handler
  -> chat application service
    -> deterministic Eino Agent runner
      -> Eino InvokableTool
        -> WatchOps mock tool
      <- normalized ToolResult / ToolError
    <- evidence-aware Agent output
  <- application result
<- ChatResponse DTO
```

### Transport Layer

Gin owns request binding, the HTTP validation entry point, status codes, and JSON response formatting. The handler does not select or invoke tools directly.

### Application Layer

`internal/application/chat` validates the normalized command, preserves request and session IDs, invokes the Agent runner, and returns an application result.

### Agent Layer

`internal/agent/eino` owns the temporary deterministic routing rules:

- `error rate` invokes `query_metrics` and `query_logs`.
- `trace` or `slow` invokes `query_traces`.
- Runbook-like language invokes `search_knowledge`.
- Unmatched requests return `MORE_CONTEXT_REQUIRED` without fabricating evidence.

The Agent calls tools only through Eino's official abstraction.

## Evidence Rules

- Successful tool evidence is copied into the answer.
- Factual conclusions reference evidence IDs.
- Cross-tool inferences reference all supporting evidence IDs.
- Failed tools create no evidence.
- Tool failures appear in both `tool_runs` and `limitations`.
- Every successful skeleton response states that it uses mock data.

## Error Handling

- Invalid HTTP bodies return the common `INVALID_ARGUMENT` envelope.
- Normalized request validation also returns `INVALID_ARGUMENT`.
- Existing `ToolError` codes are preserved in tool summaries and limitations.
- Unexpected tool or Agent errors are converted to safe internal messages.
- Raw internal errors are never returned in the HTTP response.

## Consequences

### Positive

- API and answer contracts are validated before LLM integration.
- Evidence provenance and partial failure behavior are testable.
- Gin, application, Agent, and tool ownership remain separated.
- Tests are deterministic and require no external services.

### Trade-offs

- Keyword routing is intentionally limited and is not natural-language reasoning.
- Service-name extraction supports only simple skeleton examples.
- `trace_id` remains empty until real OpenTelemetry tracing is implemented.
- There is no session continuity until Redis is added.

## Deferred Work

- Real Eino `ChatModel` calls
- Production ReAct Agent and Eino Graph orchestration
- Versioned `PromptTemplate` assets
- Redis session memory
- Elasticsearch RAG
- MySQL long-term memory and feedback
- OpenTelemetry exporter and Jaeger

## Independence

The Chat and Agent skeleton is independently designed for WatchOps-Lite. It does not copy Pilot project structure, prompts, comments, source code, or documentation.
