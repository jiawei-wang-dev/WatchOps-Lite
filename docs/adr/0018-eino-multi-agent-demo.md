# ADR 0018: Optional Eino Graph Multi-Agent Demo

- Status: Accepted
- Date: 2026-07-04

## Context

WatchOps-Lite already has a stable Single-Agent path built with native Eino Graph, PromptTemplate, ReAct, Eino tools, Tool Guard, Tool Runtime, unified Evidence, memory, fallback controls, and tracing.

An interview/demo view benefits from making role boundaries and parallel evidence gathering visible. Building a general multi-agent framework, planner, registry, message bus, or autonomous remediation system would add complexity without improving the project’s reliability-analysis story.

## Decision

Keep Single-Agent as the default and add a separate optional 3+1 Eino Graph:

1. Triage identifies the likely service, incident type, and bounded evidence plan.
2. Evidence analyzes current observability sources through existing Eino tools.
3. Knowledge retrieves runbooks and bounded confirmed long-term memory.
4. Synthesis produces the final evidence-bound answer.

Evidence and Knowledge execute as native Eino fan-out branches. A deterministic fan-in merge deduplicates evidence, tool runs, and limitations before synthesis.

The existing Tool Guard and Tool Runtime remain the only tool execution-control path. Agents do not become tools, and tools do not become agents. Synthesis may cite only evidence IDs present in the merged findings.

The existing `/api/v1/chat` and `/api/v1/chat/stream` contracts and behavior remain unchanged. The optional mode uses dedicated JSON and SSE endpoints and a console mode switch.

## Safety and Observability

- Individual role failure becomes a bounded step failure and limitation where safe.
- Zero evidence must produce limitations rather than an observed root-cause claim.
- Concurrent branch events use a serialized SSE writer.
- Events and spans contain role names, status, counts, and duration, not prompts, private reasoning, raw model output, or raw tool arguments.
- Deterministic behavior keeps the demo usable without an external model key.

## Consequences

Benefits:

- The role split and Eino Graph fan-out/fan-in are visible and testable.
- Existing tools, evidence contracts, fallback behavior, and safety layers are reused.
- Single-Agent compatibility is easy to prove.

Tradeoffs:

- This graph is intentionally domain-specific and is not dynamic.
- There is no inter-agent free-form conversation or learned routing.
- It does not claim production distributed execution, automatic remediation, or general-purpose multi-agent orchestration.

## Deferred

- Production scheduling and distributed execution
- Dynamic role discovery or planning
- Agent-to-agent protocols
- Automatic remediation
- Multi-agent cost optimization and learned policies
