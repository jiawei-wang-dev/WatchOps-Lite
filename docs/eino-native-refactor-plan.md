# Native Eino Orchestration Refactor Plan

## Audited Version

WatchOps-Lite pins `github.com/cloudwego/eino v0.9.12`. The audit used the pinned module source in the Go module cache rather than assumptions about another Eino release.

## Available Native APIs

- `compose.NewGraph[I, O]`, typed nodes, edges, graph compilation, and `Runnable.Invoke`
- `compose.NewChain[I, O]`
- `compose.InvokableLambda` for typed application adapter nodes
- `prompt.DefaultChatTemplate` through `prompt.FromMessages`
- `compose.ToolsNode` and Eino Tool interfaces
- `react.NewAgent` with bounded graph steps and Eino Tool Calling
- `callbacks.Handler` and `compose.WithCallbacks` for graph and node lifecycle instrumentation

## Eino Responsibilities

- A compiled `compose.Graph[chat.Command, chat.Result]` orchestrates the Chat request.
- Typed Lambda nodes load context, build Agent input, invoke the existing ReAct runner, collect its normalized evidence, persist session memory, and construct the unchanged response.
- The existing native Eino `DefaultChatTemplate` renders the versioned Agent prompt.
- The existing Eino ReAct Agent owns the reasoning and tool-calling loop.
- Existing tools remain registered through Eino Tool interfaces.
- Per-invocation Eino callbacks create OpenTelemetry spans for the graph and its nodes.

## WatchOps-Lite Responsibilities

- Gin transport and public DTOs
- Redis session adapter and deterministic/LLM summary fallback
- Prometheus, Elasticsearch, and Jaeger adapters
- MySQL feedback and eval persistence
- Tool Runtime reliability boundaries
- `ToolResult`, `ToolError`, and Evidence normalization
- Structured Chat response validation and demo fixtures

## Migration Boundaries and Risks

- MySQL long-term memory was implemented after this audit. The native graph's `load_long_term_memory` node now performs bounded confirmed-memory search and safely degrades when MySQL is unavailable.
- Eino ChatTemplate accepts a variables map and returns messages, while the Chat graph must retain application state for later Redis persistence. The graph therefore uses a typed Lambda render node that calls the existing native `DefaultChatTemplate`; it does not introduce a custom template engine.
- The deterministic runner has no model prompt. The render node is a safe no-op for runners that do not implement prompt rendering.
- The existing ReAct graph, Tool Runtime, evidence schema, public API, and deterministic fallback remain unchanged.
- Callback telemetry records node names, status, and errors only. It must not attach full prompts, user messages, tool output, or model output.

Native Eino Graph supports this migration cleanly. No custom workflow framework, planner, policy engine, correlation engine, MCP, or UEM layer is required.
