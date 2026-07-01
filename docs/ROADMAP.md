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

Exit status: complete. Phase 8 subsequently added the real `ChatModel`, versioned `PromptTemplate`, bounded Eino ReAct Graph, and request-time deterministic fallback.

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

Exit status: complete. Phase 9 subsequently added local Docker Compose provisioning. Durable ingestion status, deletion, deduplication, access control, and file extraction remain future extensions.

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

Exit status: complete. Phase 8 subsequently added prompt-rendering and model-call spans. Prometheus metrics, Grafana dashboards, alerting, advanced sampling, production collector deployment, and structured log correlation remain deferred.

## Phase 8: Eino ReAct Agent — Completed

Delivered:

- OpenAI-compatible Eino `ToolCallingChatModel`
- Versioned `watchops_agent_v1` Eino PromptTemplate
- Bounded Eino ReAct Graph using the four existing tools
- Collection and normalization of actual `ToolResult` messages
- Structured final-output parser with evidence-ID allowlisting
- Deterministic startup and request-time fallback
- Agent, prompt, model, tool-call, parser, and fallback tracing

Exit criteria:

- The Eino runner can invoke an existing Eino tool and return its evidence.
- Invalid evidence references cannot enter conclusions or inferences.
- Missing configuration and runtime model failure do not break Chat.
- The deterministic runner remains independently testable.

Exit status: complete. Multi-agent workflows, planning agents, LLM session summarization, automatic eval execution, and advanced token/cost budgets remain deferred.

## Phase 9: MVP Demo Packaging — Completed

Delivered:

- Docker Compose for Redis, Elasticsearch, MySQL, and Jaeger
- Full local example configuration with deterministic Agent mode and no LLM key
- Checkout reliability runbook seed data
- Reproducible knowledge, Chat, feedback, and eval-case scripts
- Combined formatting, module, test, vet, and diff verification script
- GitHub-ready README, final API examples, architecture updates, and ADR 0009

Exit criteria:

- A new contributor can start local dependencies and the application from documented commands.
- The demo proves ingestion, retrieval, Agent tools, Redis context, feedback/eval persistence, and tracing.
- Advanced services remain clearly identified as deferred rather than implied by the MVP.

Exit status: complete. The repository is packaged as a local portfolio demo; production hardening remains outside this phase.

## Upgrade 1.1: Elasticsearch-backed Logs Tool — Completed

Delivered:

- Configurable `mock` or `elasticsearch` logs backend
- Bounded service, time-range, level, keyword, and result-limit query construction
- Elasticsearch logs index mapping and normalized log-event model
- Real log evidence with trace/span metadata
- Explicit mock fallback or structured dependency error
- Checkout JSONL fixtures and repeatable Bulk API seed script
- `logs.search` and `elasticsearch.logs.search` tracing

Exit criteria:

- `query_logs` returns Elasticsearch evidence when configured and available.
- Backend failure does not block startup.
- Fallback behavior is explicit and testable.
- Metrics and traces remain unchanged.

Exit status: complete.

## Upgrade 1.2: Prometheus-backed Metrics Tool — Completed

Delivered:

- Configurable `mock` or `prometheus` metrics backend
- Allowlisted checkout reliability queries selected from tool intent
- Prometheus HTTP API vector parsing and normalized metric samples
- Real metric evidence with values, labels, queries, and timestamps
- Explicit mock fallback or structured dependency error
- Go demo metrics exporter, Prometheus scrape configuration, and Compose services
- `metrics.query` and `prometheus.query` tracing

Exit criteria:

- `query_metrics` returns Prometheus evidence when configured and available.
- Backend failure does not block startup.
- Fallback behavior is explicit and testable.
- Elasticsearch-backed logs continue unchanged.
- Traces remain mock.

Exit status: complete.

## Upgrade 1.3: Jaeger-backed Traces Tool — Completed

Delivered:

- Configurable `mock` or `jaeger` traces backend
- Exact trace-ID lookup and bounded service/time/operation search
- Minimal Jaeger Query API parsing for spans, processes, references, tags, timing, and errors
- Error-first, duration-descending span evidence ranking
- Real trace evidence with trace/span metadata
- Explicit mock fallback, no-data warning, or structured dependency error
- Reproducible trace-correlation demo script
- `traces.query` and `jaeger.query` tracing

Exit criteria:

- `query_traces` returns Jaeger evidence when configured and available.
- Backend failure does not block startup.
- Fallback behavior is explicit and testable.
- A demo Chat response can include real metrics, logs, knowledge, and trace evidence.

Exit status: complete.

## Enhancement Stage 1: LLM Session Summary — Completed

Delivered:

- Versioned JSON-only session-summary prompt
- Structured LLM summary parsing into the existing `session.Summary`
- Deterministic fallback for disabled or incomplete configuration, model errors, timeouts, and invalid JSON
- `summary.llm`, `summary.parse`, and `summary.fallback` spans
- Preserved Redis summary versioning and unchanged Chat API

Exit criteria:

- Deterministic mode remains the default.
- LLM mode cannot make Chat fail solely because summarization failed.
- Confirmed-fact safety and identifier preservation are explicit prompt rules.
- Existing ChatService and Redis summary tests continue to pass.

Exit status: complete.

## Enhancement Stage 2: Hybrid Knowledge Retrieval — Completed

Delivered:

- Optional embedding provider abstraction with deterministic test provider and OpenAI-compatible implementation
- Optional dense-vector chunk indexing
- `bm25`, `vector`, and `hybrid` retrieval modes
- Reciprocal rank fusion with BM25, vector, and RRF score metadata
- Explicit BM25 fallback when vector retrieval is unavailable
- Embedding, BM25, vector, and fusion tracing
- Backward-compatible knowledge ingestion and search APIs

Exit criteria:

- BM25 remains the dependency-free default.
- Existing chunks without embeddings remain searchable.
- Hybrid vector failure does not break knowledge search when fallback is enabled.
- Reranking remains deferred.

Exit status: complete.

## Enhancement Stage 3: Rule-based Eval Runner — Completed

Delivered:

- Synchronous bounded execution of stored eval cases through ChatService
- Deterministic evidence, tool-run, limitation, structure, tool-error, and forbidden-pattern checks
- Durable `eval_runs` and `eval_case_results`
- Run creation, summary, and result HTTP endpoints
- Reproducible `demo_eval_run.sh`
- `eval.run`, `eval.case.execute`, and `eval.case.check` spans

Exit criteria:

- Existing feedback and eval-case APIs remain compatible.
- Run reports persist pass/fail counts and per-case reasons.
- No LLM judge or prompt A/B interpretation is introduced.

Exit status: complete.

## Enhancement Stage 4: Runtime Prometheus Metrics — Completed

Delivered:

- Optional `GET /metrics` endpoint backed by a private Prometheus registry
- HTTP and Chat request counters and latency histograms
- Tool call, duration, and structured-error metrics
- Knowledge RAG search latency
- Session-memory unavailability and Agent/summary fallback counters
- Eval run completion counters
- Local Prometheus scrape configuration for WatchOps-Lite

The Prometheus evidence backend used by `query_metrics` remains separate: it answers reliability questions about monitored services, while these runtime metrics describe WatchOps-Lite itself.

Exit status: complete. Grafana visualization remains the next enhancement stage.

## Enhancement Stage 5: Grafana Dashboard — Completed

Delivered:

- Loopback-bound Grafana service in Docker Compose
- File-provisioned Prometheus datasource
- Version-controlled WatchOps-Lite Runtime dashboard
- Panels for HTTP, Chat, tools, RAG, memory, fallback, and eval runtime signals
- Anonymous viewer access scoped to the local demo

Exit status: complete. The dashboard demonstrates runtime instrumentation; production alerts, recording rules, and SLO design remain outside this stage.

## Milestone Dependencies

```mermaid
flowchart LR
    P1["P1 Gin HTTP (complete)"] --> P2["P2 Eino Tools"]
    P2 --> P3["P3 Chat + Eino Agent"]
    P3 --> P4["P4 Redis Context"]
    P4 --> P5["P5 Elasticsearch RAG (complete)"]
    P5 --> P6["P6 MySQL Feedback + Eval Seed (complete)"]
    P6 --> P7["P7 OTel + Jaeger (complete)"]
    P7 --> P8["P8 Eino ReAct Agent (complete)"]
    P8 --> P9["P9 MVP Demo Packaging (complete)"]
    P9 --> U11["Upgrade 1.1 ES Logs (complete)"]
    U11 --> U12["Upgrade 1.2 Prometheus Metrics (complete)"]
    U12 --> U13["Upgrade 1.3 Jaeger Traces (complete)"]
    U13 --> S1["Stage 1 LLM Summary (complete)"]
    S1 --> S2["Stage 2 Hybrid Retrieval (complete)"]
    S2 --> S3["Stage 3 Eval Runner (complete)"]
    S3 --> S4["Stage 4 Runtime Metrics (complete)"]
    S4 --> S5["Stage 5 Grafana Dashboard (complete)"]
```

## Deferred Work

- Advanced trace critical-path, dependency-graph, and anomaly analytics
- MySQL long-term memory, audit records, and document lifecycle metadata
- Eval-case review/export, release comparison, and optional future LLM judge
- Multi-agent orchestration
- Automated production changes
- Model fine-tuning
- Cross-region high availability
- Advanced tenant billing
- Voice and multimodal input

The starter Grafana dashboard is complete. Upgrade 1.2 uses Prometheus as the read-only service-metrics evidence backend, while Enhancement Stage 4 separately instruments WatchOps-Lite itself.
