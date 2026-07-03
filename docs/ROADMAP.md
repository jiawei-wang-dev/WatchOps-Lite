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
- Add RRF and reranking when eval results justify them. Completed.

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

Exit status: complete. Document metadata, redaction/review workflow, audit records, JSON export, scoring, and LLM judging remain deferred. Confirmed long-term memory was added in a later enhancement.

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
- Bounded Eino ReAct Graph using the four core evidence tools
- Collection and normalization of actual `ToolResult` messages
- Structured final-output parser with evidence-ID allowlisting
- Deterministic startup and request-time fallback
- Agent, prompt, model, tool-call, parser, and fallback tracing

Exit criteria:

- The Eino runner can invoke an existing Eino tool and return its evidence.
- Invalid evidence references cannot enter conclusions or inferences.
- Missing configuration and runtime model failure do not break Chat.
- The deterministic runner remains independently testable.

Exit status: complete. A later enhancement added an optional bounded Multi-Agent demo; production multi-agent orchestration, planning agents, automatic remediation, and advanced token/cost budgets remain deferred.

## Enhancement: Auxiliary OnCall Tools — Completed

Delivered:

- `query_alerts` for Prometheus ALERTS-backed or deterministic alert context
- `get_service_topology` for deterministic service dependency context
- Eino tool registration through the existing tool assembly path
- Tool Runtime timeout, fallback, structured error, evidence normalization, and tracing coverage
- Diagnostic Skill card update that mentions auxiliary tools only as optional checkout context

Exit criteria:

- The four core evidence tools remain metrics, logs, traces, and knowledge.
- The auxiliary tools do not introduce a planner, policy engine, correlation engine, MCP, UEM, or remediation workflow.
- Existing Chat API response schema and demo scripts remain unchanged.

Exit status: complete.

## Enhancement: Agent Failure Controller — Completed

Delivered:

- Lightweight controller for common Agent failure boundaries
- Safe defaults for max iterations, max tool calls, consecutive tool failures, total execution timeout, one-shot JSON repair, and repeated tool detection
- Execution-state tracking for tool calls, successes, failures, evidence count, limitations, repeated signatures, and elapsed time
- One local JSON repair attempt before falling back from invalid final output
- Controlled deterministic fallback for invalid final JSON, repeated tool failures, tool-call overflow, and total timeout boundaries
- Empty-evidence limitations that prevent unsupported root-cause claims
- OpenTelemetry spans for controller evaluation, JSON repair, and controlled fallback

Exit criteria:

- Public Chat API schema, evidence schema, Tool Runtime behavior, Redis/MySQL memory behavior, Eino ReAct/Tools behavior, and demo scripts remain unchanged.
- The controller does not introduce a planner, policy engine, correlation engine, multi-agent system, MCP, UEM, or auto-remediation.

Exit status: complete.

## Enhancement: Tool Guard — Completed

Delivered:

- Allowlist for the four core evidence tools and two auxiliary OnCall tools
- Read-only boundary for all current tools
- Common parameter validation for service, time window, limit/top_k, trace ID, severity, and topology depth
- Sensitive key redaction for tool payload, metadata, evidence metadata, and structured error details
- `tool.guard.validate` tracing with safe validation attributes

Exit criteria:

- Eino ReAct still chooses tools.
- Tool execution still goes through Tool Runtime.
- Public Chat API schema, Evidence schema, demo scripts, memory, feedback/eval, and ReAct graph behavior remain unchanged.
- Tool Guard is not a planner, policy engine, correlation engine, MCP, UEM, or auto-remediation layer.

Exit status: complete.

## Enhancement: Retrieval Evaluation — Completed

Delivered:

- Versioned retrieval eval cases in `testdata/retrieval_eval_cases.json`
- Lightweight `cmd/retrieval-eval` command and `scripts/eval_retrieval.sh`
- `make eval-retrieval` convenience target
- Report fields for case ID, query, retrieval mode, top-k IDs, matched keywords, hit/miss, score fields, empty recall, and pass rate
- Interview-friendly documentation in `docs/retrieval-evaluation.md`

Exit criteria:

- No public API schema, Eino ReAct, Tool Runtime, Tool Guard, Evidence, memory, feedback/eval, or demo-script behavior changes.
- The evaluation runner itself adds no new vector database or mandatory paid dependency and remains independent from planners, policy engines, MCP, UEM, multi-agent systems, and auto-remediation.

Exit status: complete.

## Enhancement: Retrieval Reranker — Completed

Delivered:

- Deterministic rule-based reranking over a larger initial recall candidate set
- Optional bounded external `/rerank` provider configured without hard-coded credentials
- Composite fallback for provider timeout, unavailability, invalid response, empty response, or missing credentials
- Explainable metadata for provider, rerank score/reason, original retrieval score, and safe fallback reason
- Retrieval Eval output for rerank provider, score, reason, and fallback
- OpenTelemetry spans for overall, external, rule-based, and fallback rerank execution

Exit criteria:

- BM25/vector/hybrid retrieval, `search_knowledge`, Evidence, Chat APIs, Eino ReAct, Tool Runtime, Tool Guard, and failure control remain intact.
- Local demo and unit tests require no external rerank provider or paid API.
- Rerank failure retains initial retrieval evidence and never invents provider results.

Exit status: complete.

## Enhancement: SSE Streaming Chat — Completed

Delivered:

- `POST /api/v1/chat/stream` Server-Sent Events endpoint
- Reuse of the existing Gin handler validation and Chat application service
- Reuse of the native Eino Chat graph, ReAct runner, Tool Runtime, Tool Guard, Agent Failure Controller, memory, evidence, feedback/eval, and tracing paths
- Bounded stream events for workflow lifecycle, Eino graph node lifecycle, memory status, tool-call status, evidence count, failure-controller activation, final answer, and workflow completion/failure
- `final_answer` event carrying the existing public Chat response JSON
- Tests for route registration, SSE content type, expected event sequence, final event ordering, and no exposed reasoning/prompt fields

Exit criteria:

- `POST /api/v1/chat` remains unchanged.
- Public Chat response schema, Evidence schema, Tool Runtime behavior, Tool Guard behavior, Agent Failure Controller behavior, memory, feedback/eval, ReAct graph behavior, and demo scripts remain unchanged.
- Streaming does not expose chain-of-thought, raw prompts, raw tool arguments, raw model output, raw sensitive logs, or unredacted tool output.
- No benchmark, frontend, MCP, UEM, policy engine, correlation engine, planner, multi-agent system, or auto-remediation is introduced.

Exit status: complete.

## Enhancement: Local Agent Benchmark — Completed

Delivered:

- Six versioned reliability-analysis cases covering metrics, logs, traces, knowledge, alerts, and topology
- Lightweight black-box benchmark CLI and `scripts/benchmark_agent.sh`
- `make benchmark-agent` convenience target
- JSON and Markdown reports under the ignored local `tmp/` directory
- Measurements for success rate, latency distribution, tool runs, evidence, detectable fallbacks, limitations, empty evidence, request/trace IDs, and one SSE final-answer check
- Honest interpretation and limitations in `docs/performance-report.md`

Exit criteria:

- Existing Chat and SSE APIs, Eino graph/ReAct behavior, runtime controls, Evidence, memory, feedback/eval, and demo scripts remain unchanged.
- The benchmark uses only existing public response fields and does not claim production throughput or readiness.
- No frontend, new infrastructure, MCP, UEM, planner, policy engine, correlation engine, multi-agent system, or auto-remediation is introduced.

Exit status: complete.

## Enhancement: Local Agent Demo Console — Completed

Delivered:

- Embedded, build-free HTML/CSS/vanilla JavaScript console at `GET /`
- Polished responsive light SaaS layout with Chat, Streaming Trace, Evidence, Knowledge / Memory, and Eval / Feedback tabs
- Existing normal Chat and SSE APIs rendered without exposing prompts, tool arguments, or private reasoning
- Evidence grouping, tool-run status, request/trace context, and local Jaeger, Grafana, and Prometheus links
- Existing knowledge search, feedback, and rule-based eval actions with graceful dependency-unavailable states
- Router tests proving static assets do not shadow health or API routes

Exit criteria:

- No public API schema or Agent, Tool Runtime, Tool Guard, failure-control, Evidence, memory, feedback/eval, demo, benchmark, or retrieval-eval behavior changes.
- No frontend framework, package manager, build system, authentication, or new backend convenience API.
- The console is explicitly a local demonstration surface, not a production frontend.

Exit status: complete.

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
- Reranking is delivered as a later, independently verified enhancement.

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

## Enhancement Stage 6: Final Demo Verification — Completed

Verified:

- all Compose services healthy, including Grafana
- real Prometheus metrics, Elasticsearch logs, Jaeger traces, and Elasticsearch knowledge evidence in Chat
- Redis recent messages and persisted rolling summary
- MySQL feedback, eval cases, eval runs, and case results
- Jaeger multi-span trace tree
- Prometheus scraping WatchOps-Lite runtime metrics
- provisioned Grafana datasource and 11-panel dashboard
- complete repository verification gate

Exit status: complete. WatchOps-Lite enhanced demo is fully verified locally.

## Backend Convergence: Native Eino Graph and Business Skills — Completed

Delivered:

- Compiled `compose.Graph[chat.Command, chat.Result]` around the existing Eino ReAct runner
- Native typed nodes for context, prompt rendering, ReAct, evidence, memory, and response construction
- Eino callback lifecycle tracing for the graph and each node
- Native Eino PromptTemplate rendering before Eino ReAct execution
- Lightweight business Skill definitions for metrics, logs, traces, runbooks, and checkout diagnosis
- Stable bad-case reason constants for feedback/eval metadata
- Clear Tool vs Skill vs Tool Runtime documentation
- Deliberate removal of unused policy and evidence-correlation helpers

Constraints preserved:

- no public API, evidence schema, ReAct behavior, Tool Runtime, or demo-script changes
- no planner, policy learning, correlation engine, MCP, UEM, skill registry, dynamic discovery, or external dependency

Exit status: complete. Backend orchestration uses native Eino Graph, PromptTemplate, ReAct, Tools, and callbacks without introducing a custom Agent framework.

## Enhancement: MySQL Confirmed Long-term Memory — Completed

Delivered:

- `long_term_memories` MySQL schema and bounded keyword retrieval
- Domain Store and Service with `feedback_up`, `eval_good_case`, and `manual` source types
- Evidence-backed positive-feedback memory creation; downvotes never create memory
- Native Eino `load_long_term_memory` graph node before prompt rendering
- Bounded `confirmed_long_term_memories` prompt input with a default top-k of three
- Safe Chat degradation and `LONG_TERM_MEMORY_UNAVAILABLE` when enabled MySQL cannot be queried
- `longterm_memory.search` and `longterm_memory.save` spans without prompt or answer content

The implementation keeps Redis session context, MySQL cross-session confirmed memory, and Elasticsearch document RAG as separate responsibilities. Automatic model-authored memory and vector memory search are intentionally excluded.

Exit status: complete.

## Enhancement: Native Eino Parallel Context Branches — Completed

Delivered:

- Native Eino Graph fan-out after normalized Chat input
- Independent session-memory, long-term-memory, and diagnostic-skill branches
- Native `AllPredecessor` fan-in before prompt rendering
- Branch, merge, and downstream lifecycle spans through existing Eino callbacks
- SSE visibility for the new graph-node lifecycle without changing the public Chat response
- Concurrency and compatibility tests covering the compiled Eino graph

The implementation uses Eino's DAG scheduling directly. It does not add a custom workflow abstraction, application-managed branch goroutines, a planner, or a policy engine. ReAct, Tool Guard, Tool Runtime, Failure Controller, memory behavior, streaming, and API contracts remain unchanged.

Exit status: complete.

## Enhancement: Chat History API and Console Integration — Completed

Delivered:

- `GET /api/v1/chat/history` with a default limit of 20 and maximum of 100
- `DELETE /api/v1/chat/history` for session-scoped Redis recent messages and rolling summary
- Structured summary, message timestamps, roles, request IDs, and bounded metadata
- Explicit separation from confirmed MySQL long-term memory, knowledge, feedback, and eval data
- Embedded console history load, confirm-before-clear, conversation rendering, and automatic refresh after Chat/SSE completion
- Session-history tracing and Redis/application/HTTP regression coverage

The feature reuses the existing Redis session Store and static no-build frontend. It does not add a conversation table, frontend framework, package manager, or change existing Chat and streaming response contracts.

Exit status: complete.

## Enhancement: Lightweight User Profile Context — Completed

Delivered:

- Minimal `user_profiles` MySQL model and upsert/get Store
- Optional `user_id` Chat request field
- Native Eino `graph.load_user_profile` parallel context branch
- Bounded prompt context for default service, related services, timezone, and scalar preferences
- Graceful skip for absent, missing, or unavailable profiles

Profiles are contextual OnCall preferences only. They do not provide authentication, accounts, RBAC, tenancy, or a public management API, and profile metadata is never dumped into the Agent prompt.

Exit status: complete.

## Enhancement: Demo Prometheus Alert Rules — Completed

Delivered four local rules for checkout error rate, checkout latency, payment timeouts, and Redis latency. Prometheus mounts and evaluates the rules, while `query_alerts` reads standard `ALERTS` data with its existing mock fallback. Alertmanager, paging, and production incident routing remain intentionally excluded.

Exit status: complete.

## Enhancement: Demo Log Generator — Completed

Delivered a dependency-light Go JSONL generator with checkout timeout, payment error, and Redis latency scenarios; bounded event counts; current-time windows; and deterministic `--seed` plus `--now` output. The existing fixture remains unchanged, and `demo_seed_logs.sh` accepts an optional generated JSONL path while preserving its default behavior.

Exit status: complete.

## Enhancement: Local Dependency Check — Completed

Delivered `make check-deps` for Go, Docker/Compose, curl, Python, local configuration, embedded web assets, demo fixtures, Prometheus/Grafana configuration, expected ports, and optional service reachability. Tooling and required repository files are hard failures; services not yet running are warnings. The check requires neither internet access nor external API keys.

Exit status: complete.

## Enhancement: End-to-End Demo Check — Completed

Delivered `make e2e-demo`, a thin orchestrator over existing dependency, seed, Chat, SSE, retrieval-eval, Agent-eval, and benchmark paths. It supports bounded skip flags, optional generated logs, clear PASS/WARN/FAIL output, report locations, and local UI URLs. It checks an already-running stack and never starts or stops Compose.

Exit status: complete.

## Enhancement: Optional Eino Graph Multi-Agent Demo — Completed

Delivered:

- Optional `/api/v1/chat/multi-agent` and `/api/v1/chat/multi-agent/stream` endpoints
- Bounded Triage, Evidence, Knowledge, and Synthesis diagnostic roles
- Native Eino Graph fan-out for Evidence/Knowledge and fan-in before synthesis
- Reuse of existing Eino tools, Tool Guard, Tool Runtime, Failure Controller, and unified Evidence
- Stable evidence/limitation merge and evidence-ID allowlisting at synthesis
- Serialized SSE events for concurrent branches
- Build-free bilingual console mode and four role cards
- Lightweight English and Chinese E2E targets

Single-Agent remains the default and its API, prompt, ReAct graph, Redis memory, Tool Runtime, Evidence schema, and demo scripts remain unchanged. This capability is an interview/demo graph, not a planner, multi-agent platform, autonomous remediation system, or production-distributed orchestration claim.

Exit status: implementation complete; final local enhanced-demo verification is tracked separately.

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
    S5 --> S6["Stage 6 Enhanced Demo Verification (complete)"]
    S6 --> C1["Backend convergence (complete)"]
    C1 --> M1["MySQL confirmed memory (complete)"]
    M1 --> MA["Optional Eino Multi-Agent demo (complete)"]
```

## Deferred Work

- Advanced trace critical-path, dependency-graph, and anomaly analytics
- MySQL audit records and document lifecycle metadata
- Eval-case review/export, release comparison, and optional future LLM judge
- Production multi-agent orchestration beyond the bounded local demo
- Automated production changes
- Model fine-tuning
- Cross-region high availability
- Advanced tenant billing
- Voice and multimodal input

The starter Grafana dashboard is complete. Upgrade 1.2 uses Prometheus as the read-only service-metrics evidence backend, while Enhancement Stage 4 separately instruments WatchOps-Lite itself.
