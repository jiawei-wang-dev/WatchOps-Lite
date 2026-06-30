# WatchOps-Lite Development Roadmap

The roadmap introduces one architectural capability at a time. Each phase should remain runnable and testable, and no later-phase infrastructure should be added early merely to complete the directory tree.

## Phase 1: Gin HTTP Skeleton — Completed

Delivered:

- Go module and minimal package structure
- Gin router and `GET /healthz`
- Request logging, request IDs, and panic recovery middleware
- JSON configuration with environment overrides
- Structured logging
- Graceful startup and shutdown
- OpenTelemetry lifecycle placeholder
- Unit tests and developer commands

Exit status: complete.

## Phase 2: Eino Tool Skeleton and WatchOps Tool Contracts — Completed

Delivered:

- Add Eino as the Agent/LLM framework dependency.
- Use Eino's Tool abstraction for schema exposure, registration, and invocation.
- Define business-level input and output contracts for the initial tools.
- Define structured `ToolError`.
- Add a shared timeout/fallback execution wrapper and safe internal-error normalization.
- Introduce deterministic fixture-based tool tests.

Initial tool contracts:

- `query_logs`
- `query_metrics`
- `query_traces`
- `search_knowledge`

Constraints:

- Do not build a custom Tool Registry.
- Do not connect production observability or knowledge backends yet.
- Eino owns registration and calling; WatchOps-Lite owns execution policy and business contracts.

Exit criteria:

- Eino can expose and invoke fixture-backed tools.
- Invalid input produces a structured validation error.
- Timeout behavior is deterministic and tested.

Exit status: complete. Redaction, output-size enforcement, and tracing hooks remain extension points for the real-connector and observability phases.

## Phase 3: Chat API and Eino Agent Skeleton — Completed

Delivered:

- `POST /api/v1/chat`
- Thin Gin Chat handler and transport DTOs
- Chat application service
- Deterministic Eino `InvokableTool` runner
- Transparent routing for error-rate, trace/slow, and runbook questions
- Structured answer sections, evidence IDs, limitations, and tool run summaries
- Safe HTTP and tool error mapping

Exit criteria:

- An error-rate Chat request completes end to end.
- The skeleton invokes Phase 2 tools through Eino.
- Conclusions reference returned evidence.
- Failed tools appear in limitations without fabricated evidence.

Exit status: complete. Real `ChatModel`, `PromptTemplate`, Eino Graph, and production ReAct orchestration remain deferred.

## Phase 4: Redis Session Memory — Completed

Delivered:

- Recent-message sliding window
- Rolling structured session summary
- Session TTL and deletion behavior
- Optimistic summary versioning
- Context budget and pruning
- Redis in the local Docker Compose stack

Exit criteria:

- Multi-turn conversations retain confirmed facts beyond the raw-message window.
- Concurrent summary updates do not silently overwrite newer state.
- Redis failure produces an explicit single-turn degradation.

Exit status: complete. The current summary implementation is deterministic; an LLM-based summary model remains deferred.

## Phase 5: RAG with Elasticsearch — Completed

Delivered:

- Plain-text/Markdown document ingestion and metadata lookup
- Deterministic paragraph-first chunking
- Elasticsearch chunk indexing
- BM25-first knowledge search API and `search_knowledge` implementation
- Metadata filters, evidence IDs, and graceful mock fallback
- Optional Elasticsearch configuration that does not block startup

Evolution path:

- Add embeddings and vector search.
- Add hybrid lexical/vector retrieval.
- Add RRF and reranking when eval results justify them.

Exit criteria:

- A fixed runbook reliably returns the expected source chunk.
- Elasticsearch failures are sanitized and do not prevent application startup.
- Existing Chat and health behavior remains intact.

Exit status: complete. Durable ingestion status, deletion, deduplication, access control, file extraction, and Docker Compose Elasticsearch provisioning remain future extensions.

## Phase 6: MySQL Feedback and Eval Seed — Completed

Delivered:

- Optional `database/sql` MySQL client with bounded connection settings
- Minimal `feedback` and `eval_cases` schema initialization
- Upvote/downvote records with reasons, corrections, answer snapshots, evidence IDs, tool summaries, and metadata
- Manual good-case and bad-case creation from compatible feedback
- Feedback create/get and eval create/list APIs
- Structured unavailable behavior without blocking application startup

Exit criteria:

- Feedback retains the answer and execution context needed for later review.
- Downvotes can seed bad cases without treating the incorrect answer as gold truth.
- Upvotes can seed good cases for future regression coverage.
- MySQL failures do not break Chat, health, or knowledge endpoints.

Exit status: complete. Long-term memory, document metadata, redaction/review workflow, audit records, JSON export, automatic evaluation, scoring, and LLM judging remain deferred.

## Phase 7: OpenTelemetry and Jaeger Tracing — Completed

Delivered:

- OpenTelemetry SDK and OTLP exporter
- Local Jaeger all-in-one instructions with OTLP gRPC ingestion
- Parent-based ratio sampling and service/environment resource attributes
- W3C trace-context extraction and `X-Trace-ID` responses
- Spans for:
  - Agent runs
  - Session context load, persistence, and summary updates
  - RAG ingestion, search, and Elasticsearch calls
  - Tool execution
  - Feedback and eval processing
- Safe trace attributes and redaction rules
- Chat response trace IDs
- No-op behavior when disabled and non-blocking exporter failures

Exit criteria:

- A Chat trace is visible end to end in Jaeger.
- Tool timeout and fallback events are visible without exposing sensitive data.
- Telemetry export failure does not block business requests.

Exit status: complete. Prompt-rendering and model-call spans will be added when those runtime stages exist. Prometheus metrics, Grafana dashboards, alerting, advanced sampling, production collector deployment, and structured log correlation remain deferred.

## Milestone Dependencies

```mermaid
flowchart LR
    P1["P1 Gin HTTP (complete)"] --> P2["P2 Eino Tools"]
    P2 --> P3["P3 Chat + Eino Agent"]
    P3 --> P4["P4 Redis Context"]
    P4 --> P5["P5 Elasticsearch RAG (complete)"]
    P5 --> P6["P6 MySQL Feedback + Eval Seed (complete)"]
    P6 --> P7["P7 OTel + Jaeger (complete)"]
```

## Deferred Work

- Prometheus application metrics
- MySQL long-term memory, audit records, and document lifecycle metadata
- Eval-case review/export and automatic regression runner
- Multi-agent orchestration
- Automated production changes
- Model fine-tuning
- Cross-region high availability
- Advanced tenant billing
- Voice and multimodal input

Prometheus may be added after the MVP when concrete service-level metrics and dashboards have been identified.
