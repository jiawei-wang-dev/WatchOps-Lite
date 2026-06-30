# ADR 0001: Framework and Technology Stack

- Status: Accepted
- Date: 2026-06-30

## Context

WatchOps-Lite is an independently designed Agentic RAG assistant for service reliability analysis. Its feature category overlaps with capabilities that mature frameworks and infrastructure products already solve well.

Building custom replacements for routing, Agent orchestration, ReAct-style tool calling, tool schema exposure, knowledge storage, session caching, durable metadata storage, or tracing would increase implementation time and maintenance risk without improving the project's core value.

The project therefore needs a clear boundary between capabilities delegated to established frameworks and the reliability-analysis policies owned by WatchOps-Lite.

## Decision

WatchOps-Lite will prefer mature, widely used frameworks and libraries where they satisfy the requirement. It will not reinvent infrastructure or orchestration components solely to make them project-specific.

The accepted stack is:

| Concern | Decision |
| --- | --- |
| HTTP | Gin |
| Agent orchestration | Eino |
| Agent style | Eino Graph plus Eino-based ReAct-style tool calling |
| Knowledge retrieval | Elasticsearch |
| Session memory | Redis |
| Durable business storage | MySQL |
| Tracing | OpenTelemetry with Jaeger |
| Metrics | Prometheus after the MVP, if needed |
| Local deployment | Docker Compose |

### Gin

Gin is responsible for:

- HTTP routing and route groups
- Middleware
- Request binding
- Validation entry points
- HTTP response formatting

Handlers remain thin. They translate between HTTP and application-level operations; they do not contain Agent, retrieval, memory, or feedback business logic.

### Eino

Eino is responsible for:

- Agent workflow orchestration
- Graph construction and execution
- `PromptTemplate` rendering
- `ChatModel` integration and model invocation
- Tool schema exposure and Tool abstraction
- Tool registration and calling
- ReAct-style Agent execution

WatchOps-Lite will not build a competing Tool Registry when Eino already provides the required registration and invocation mechanism.

WatchOps-Lite remains responsible for:

- Business-level input and output contracts for each tool
- Structured `ToolError`
- Timeout, bounded retry, and fallback behavior
- Evidence normalization and citation metadata
- Sensitive-value redaction
- Output size control and truncation metadata
- OpenTelemetry wrappers around tool execution
- Domain-specific authorization and query constraints

These responsibilities wrap or adapt Eino tools; they do not recreate Eino's tool runtime.

### Elasticsearch

Elasticsearch is the knowledge retrieval backend. Documents are indexed as source-addressable chunks.

The retrieval path evolves incrementally:

1. BM25 retrieval for the first usable MVP
2. Vector retrieval when embeddings are introduced
3. Hybrid lexical and vector retrieval
4. RRF and reranking when evaluation data demonstrates value

Elasticsearch-specific query construction stays behind the retrieval/platform boundary.

### Redis

Redis stores short-lived session context:

- Recent messages in a sliding window
- Rolling structured session summaries
- Optional short-lived coordination and idempotency data

Redis is not the durable source of truth for long-term memory.

### MySQL

MySQL stores durable business state:

- Long-term memory
- Feedback
- Document metadata and ingestion status
- Eval candidates and review state
- Audit records

### OpenTelemetry and Jaeger

OpenTelemetry instruments important execution stages. Jaeger is the local trace visualization backend.

Tracing covers:

- End-to-end Agent runs
- Context building
- RAG search and ranking
- Tool execution
- Prompt rendering
- Model calls
- Feedback processing

Prometheus metrics may be added after the MVP. Metrics are not a prerequisite for the tracing baseline.

### Docker Compose

Docker Compose is the standard local deployment mechanism. Elasticsearch, Redis, MySQL, the OpenTelemetry Collector, and Jaeger are added to the local stack as their implementation phases arrive.

## Consequences

### Positive

- Less custom infrastructure code to maintain
- Faster delivery of the reliability-analysis behavior that differentiates the project
- Better alignment with established Go and observability ecosystems
- Clear framework ownership and application boundaries
- Easier local setup through a documented Compose stack

### Trade-offs

- Framework upgrades and compatibility must be managed deliberately.
- Eino and vendor clients must remain behind project boundaries to limit coupling.
- Some framework defaults will need wrappers to enforce evidence, redaction, timeout, and fallback policy.
- Elasticsearch, Redis, MySQL, and Jaeger increase the eventual local resource footprint.

## Independence and Originality

This decision selects technologies, not an implementation to copy. WatchOps-Lite must not copy the Pilot project structure, prompts, comments, source code, or documentation.

The project independently implements the same category of engineering ideas through its own architecture, business contracts, evidence policy, evaluation loop, package boundaries, and documentation.
