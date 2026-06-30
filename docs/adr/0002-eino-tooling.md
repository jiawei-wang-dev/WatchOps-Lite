# ADR 0002: Eino Tooling and WatchOps Tool Contracts

- Status: Accepted
- Date: 2026-06-30

## Context

Phase 2 introduces the first Agent-facing components without implementing a Chat API, a ReAct Agent, or real reliability-data integrations.

Eino already provides the Tool abstraction, schema inference, metadata exposure, JSON argument decoding, result encoding, and the mechanism future Agents will use to call tools. Rebuilding those capabilities would conflict with ADR 0001.

WatchOps-Lite still needs stable business contracts and reliability policies that are independent of any logs, metrics, traces, or knowledge vendor.

## Decision

WatchOps-Lite uses Eino's official `tool.InvokableTool` and `utils.InferTool` APIs to expose tools. `BuildMockTools` returns Eino tools directly. The project does not maintain a parallel custom Tool Registry.

Eino owns:

- Tool metadata and schema exposure
- Typed argument decoding
- Tool registration and invocation for future Agent execution
- Result encoding for model-facing tool messages

WatchOps-Lite owns:

- Typed business-level input and output contracts
- Normalized `ToolResult`
- Structured `ToolError`
- Evidence normalization
- Timeout and fallback policy
- Sanitization of unexpected internal errors
- Redaction and output-size policies as real connectors are introduced
- OpenTelemetry hooks added during the observability phase

## Shared Result Contract

`ToolResult` identifies the tool and contains:

- Success state
- Normalized evidence
- Optional payload
- Warnings
- Metadata
- Structured error on failure
- Start and finish timestamps
- Duration in milliseconds

`EvidenceItem` preserves source type, source name, time range, content, resource identity, optional score or confidence, and metadata.

`ToolError` uses the initial codes:

- `TOOL_INVALID_ARGUMENT`
- `TOOL_TIMEOUT`
- `TOOL_DEPENDENCY_UNAVAILABLE`
- `TOOL_RATE_LIMITED`
- `TOOL_INTERNAL`

Unexpected errors are converted to a safe `TOOL_INTERNAL` response. Raw backend errors must not be exposed to a model or API consumer.

## Phase 2 Tools

The current implementation exposes deterministic mocks:

- `query_logs`
- `query_metrics`
- `query_traces`
- `search_knowledge`

Each tool has a typed input, validates required fields, returns normalized mock evidence, and calls no external backend.

The timeout helper applies a default deadline while allowing a per-tool timeout later. It respects parent context cancellation and returns a structured timeout result when its deadline is exceeded.

## Consequences

### Positive

- Tool schemas come from official Eino mechanisms.
- Business contracts can be tested without an LLM or infrastructure.
- Future real adapters can preserve the same normalized result shape.
- Structured errors and timeouts exist before external dependencies are added.

### Trade-offs

- Eino errors may wrap the underlying WatchOps `ToolError`; callers should use Go error unwrapping where needed.
- Mock timestamps and execution duration are runtime values even though evidence content is deterministic.
- Redaction, output-size enforcement, and tracing hooks remain policy extension points until real connectors and OpenTelemetry are introduced.

## Deferred Work

This phase does not implement:

- Chat API or ReAct Agent
- Eino Graph workflows
- LLM or `ChatModel` calls
- Elasticsearch, Redis, MySQL, Prometheus, Jaeger, or production telemetry
- Real logs, metrics, traces, or knowledge queries

Real connectors will be introduced in their roadmap phases and must keep these business contracts and safety policies.

## Independence

This implementation is designed for WatchOps-Lite and uses only Eino's public framework APIs. It does not copy Pilot project structure, prompts, comments, source code, or documentation.
