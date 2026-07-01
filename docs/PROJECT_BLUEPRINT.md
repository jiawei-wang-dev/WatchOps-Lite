# WatchOps-Lite Project Blueprint

## 1. Product Definition

WatchOps-Lite is an Agentic RAG assistant for service reliability analysis. A user can ask about an incident, alert, or abnormal condition. The system retrieves internal knowledge, queries logs, metrics, and traces as needed, and returns an analysis with evidence, confidence, and recommended next steps.

This project is independently designed from its own requirements. It does not reproduce the implementation, layout, prompts, comments, or documentation of any training or example project.

### 1.1 Target Users

- On-call engineers who need to establish incident context quickly
- SREs investigating issues across logs, metrics, and traces
- Development teams looking for previously validated remediation knowledge
- Open-source readers interested in a complete Agent engineering feedback loop

### 1.2 Core Value

- Reduce context switching between observability systems.
- Make every important claim traceable to source evidence.
- Retain important facts in long conversations while controlling context cost.
- Convert user feedback into repeatable regression evaluation assets.

## 2. MVP Use Cases

### A. Initial Alert Diagnosis

A user asks, “Why did the checkout service error rate increase over the last 20 minutes?” The Agent extracts the service and time range, queries metrics to identify the anomaly window, queries logs to summarize error classes, optionally searches traces for bottlenecks, and cites a relevant runbook.

### B. Knowledge Retrieval

A user submits Markdown or plain-text runbook content. The system chunks and indexes it in Elasticsearch. Later, `search_knowledge` returns source-attributed excerpts. File extraction and embeddings can be added after the MVP.

### C. Follow-up Questions

A user asks a follow-up question. The MVP combines recent Redis messages with a deterministic rolling session summary. MySQL long-term memory remains a future extension.

### D. Feedback Reuse

Upvotes and downvotes are stored with answer provenance. A user can manually create a compatible good or bad eval case from reviewed feedback. Redaction, review state, JSON export, and automatic regression execution remain future work.

## 3. Scope and Boundaries

### 3.1 Included in the MVP

- Go HTTP server built with Gin and a versioned REST API
- Chat requests using a bounded, Eino-based ReAct-style Agent and Graph orchestration
- JSON-based plain-text/Markdown ingestion, chunk indexing, metadata lookup, and BM25 search
- Logs, metrics, traces, and knowledge-search tools
- Redis session summaries and recent messages
- MySQL feedback and manually seeded eval cases
- OpenTelemetry tracing, Jaeger visualization for local development, and structured logging
- Docker Compose, demo data, scripted API flow, and a local verification command

### 3.2 Excluded from the MVP

- Automated changes to production systems
- Unrestricted backend query languages
- Advanced RBAC, tenant billing, and model fine-tuning
- Voice or image-based incident analysis
- Autonomous multi-agent negotiation

## 4. The Four Agent Engineering Disciplines

### 4.1 Prompt Engineering

Prompts are stored as versioned templates outside business code. At minimum, templates are separated into system policy, task planning, tool-result synthesis, and final response. Template variables use allowlisted rendering; untrusted user content is never silently inserted as instruction text.

The final response follows a stable structure:

```text
Conclusion
- Most likely cause and confidence

Evidence
- [E1] Observation, time range, and source

Inferences
- What follows from which evidence

Recommendations
- Verifiable, read-only-first next steps

Limitations
- Missing data, failed tools, and unverified assumptions
```

Evidence rules:

1. Every verifiable factual claim must reference an evidence ID.
2. Data absent from tool results must not be presented as observed fact.
3. Conflicting evidence must be shown rather than silently resolved.
4. Insufficient evidence leads to a clarifying question or an explicit uncertainty statement.
5. Citations contain source type, time range, resource identity, and an optional source URL.
6. Prompt versions are attached to traces and feedback for reproducibility.

### 4.2 Context Engineering

Context is assembled in priority order:

1. System rules and safety boundaries
2. Current user request
3. Recent raw messages in a sliding window
4. Rolling Redis session summary
5. Relevant MySQL long-term memories
6. Current tool and RAG evidence

Initial policy:

- `session:{session_id}:recent` stores the latest 12 messages in Redis.
- `session:{session_id}:summary` stores a structured summary and version.
- Exceeding a message or token threshold summarizes the oldest messages.
- Summary fields are goal, confirmed facts, open questions, attempted actions, and important entities.
- The current request and evidence receive the highest budget; older conversation is pruned first.
- Summary writes use optimistic versioning to avoid concurrent overwrite.
- The default Redis TTL is 24 hours and remains configurable.

Long-term memory only stores user-confirmed information or high-confidence extracted facts. Every item records its source, creation time, expiry, and deletion state. Model inference must never be promoted directly into long-term fact.

Phase 4 implements the Redis recent-message window and versioned rolling summary. The current summarizer is deterministic and processes messages leaving the raw window; a future LLM summary model can replace it without changing the session storage contract. Redis failures degrade Chat to the current turn and are surfaced through safe response limitations.

### 4.3 Harness Engineering

Tools use Eino's Tool abstraction for schema exposure, registration, and invocation. WatchOps-Lite does not build a parallel Tool Registry when Eino already provides the required mechanism.

WatchOps-Lite defines the business-level input and output contracts for each tool, plus a shared structured `ToolError`. Its harness wrappers add policy around Eino tools without replacing Eino's registration or calling lifecycle. The orchestration loop uses a bounded, Eino-based ReAct-style Agent: reason about the next step, call a validated tool when evidence is required, observe the result, and stop when the answer or execution budget is complete. Eino Graph is used where the workflow benefits from explicit nodes, transitions, and execution state.

The harness provides:

- JSON Schema input validation
- Per-tool timeouts
- Bounded retries only for explicitly retryable failures
- Concurrency, time-range, and result-count limits
- A reserved circuit-breaker boundary
- Explicit fallback for unavailable primary sources
- Output truncation and sensitive-value redaction
- Standard tracing, latency, and outcome recording
- Structured errors without exposing raw backend errors to the model or user

Suggested structured error:

```json
{
  "code": "TOOL_TIMEOUT",
  "message": "metrics query exceeded its deadline",
  "retryable": true,
  "tool": "query_metrics",
  "details": {
    "deadline_ms": 3000
  },
  "fallback": "ask for a narrower time range"
}
```

Suggested initial timeouts are 2 seconds for knowledge, 3 seconds for metrics, and 5 seconds each for logs and traces. These are configuration defaults, not hard-coded constants.

### 4.4 Loop Engineering

Each feedback record links to:

- Request, response, and session identifiers
- Prompt, model, and tool versions
- Evidence citations and a tool-run summary
- Like or dislike rating
- Optional reason category and free-text comment
- Optional human-corrected response

Feedback lifecycle:

```text
Production feedback -> eval candidate -> redact/review
                    -> fixed case -> offline regression -> release gate
```

- Likes create positive candidates used to detect quality regressions.
- Dislikes create bad cases retaining the failure category and expected behavior.
- A bad answer is never used as a gold answer. Without human correction, the case asserts only forbidden behavior.
- The same case can evaluate prompts, retrieval, tool routing, or the end-to-end system.
- Evaluation data must not contain credentials, personal information, or sensitive production content.

Suggested `agent_eval_cases.json` format:

```json
[
  {
    "id": "case_checkout_error_rate_001",
    "kind": "positive",
    "input": {
      "message": "Why did checkout errors increase over the last 20 minutes?",
      "fixture": "checkout_incident_a"
    },
    "expect": {
      "required_tools": ["query_metrics"],
      "required_sections": [
        "conclusion",
        "evidence",
        "recommendations",
        "limitations"
      ],
      "must_cite_evidence": true,
      "forbidden_claims": []
    },
    "tags": ["metrics", "incident-triage"]
  }
]
```

## 5. Functional Modules

### 5.1 Chat

Responsibilities: request validation, session loading, Eino-based Agent execution, evidence assembly, context persistence, and response delivery. Gin owns routing, middleware, request binding, validation entry points, and response formatting. Eino owns Agent workflow orchestration, prompt rendering, model invocation, tool calling, and graph-style execution. Handlers translate HTTP requests into application commands and must not contain business logic.

Phase 3 introduced a deterministic Chat skeleton that invokes Eino `InvokableTool` values through explicit message rules. Phase 8 adds the production Eino `ChatModel`, versioned `PromptTemplate`, and bounded ReAct Graph while preserving the deterministic runner for tests and fallback.

`POST /api/v1/chat`

```json
{
  "session_id": "ses_01...",
  "message": "What happened to the payment service in the last 30 minutes?",
  "time_context": {
    "from": "2026-06-30T00:00:00Z",
    "to": "2026-06-30T00:30:00Z"
  }
}
```

Response:

```json
{
  "request_id": "req_01...",
  "session_id": "ses_01...",
  "answer": {
    "conclusion": [],
    "evidence": [],
    "inferences": [],
    "recommendations": [],
    "limitations": []
  },
  "tool_runs": [],
  "trace_id": "..."
}
```

### 5.2 RAG

Endpoints:

- `POST /api/v1/knowledge/documents`: upload a document
- `GET /api/v1/knowledge/documents/{id}`: reconstruct document metadata from indexed chunks
- `POST /api/v1/knowledge/search`: perform explicit retrieval

MVP processing stages: validate → normalize → chunk → synchronous Elasticsearch index.

The first MVP may use a simplified BM25 retrieval path. The indexing and retrieval contracts must leave room for embeddings, vector search, hybrid retrieval, Reciprocal Rank Fusion (RRF), and reranking without changing the Chat use case.

Each chunk records its generated identity, document ID, title, chunk index, source, metadata, content, and creation time.

### 5.3 Tools

| Tool | Purpose | Primary constraints | Suggested adapter |
| --- | --- | --- | --- |
| `query_logs` | Search and aggregate logs | Bounded time range, result count, level, and keywords | Elasticsearch with mock fallback |
| `query_metrics` | Query time-series metrics | Allowlisted metrics and functions | Prometheus HTTP API |
| `query_traces` | Find traces and important spans | Bounded services, time range, and result count | Tempo HTTP API |
| `search_knowledge` | Retrieve runbooks and postmortems | Top-k, score threshold, and access filters | Elasticsearch |

Tools are read-only by default. The model produces constrained domain parameters, and adapters construct backend queries. Arbitrary Elasticsearch DSL, LogQL, or PromQL must not pass directly from the model to a backend.

### 5.4 Memory and Feedback

Redis stores:

- Session summaries
- Recent messages
- Optional idempotency keys and short-lived locks

MySQL stores:

- `feedback`
- `eval_cases`

Elasticsearch stores chunk content and fields required for BM25 and metadata filtering. MySQL currently stores feedback and eval cases; long-term memory, document lifecycle metadata, and audit records are deferred.

## 6. Common API Conventions

- Prefix: `/api/v1`
- JSON fields: `snake_case`
- Time: UTC RFC 3339
- IDs: type-prefixed cryptographically random identifiers
- Request correlation: accept or generate `X-Request-ID`
- Idempotent upload keys remain a post-MVP extension
- Error envelope:

```json
{
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "time range is required",
    "request_id": "req_01...",
    "details": []
  }
}
```

Current public error codes include `INVALID_ARGUMENT`, `NOT_FOUND`, `DEPENDENCY_UNAVAILABLE`, and `INTERNAL`; tool limitations retain structured tool-specific codes such as `TOOL_TIMEOUT`.

## 7. Current Go Project Layout

```text
WatchOps-Lite/
├── cmd/server/                 # HTTP entry point; startup only
├── configs/                    # Default and full local-demo configuration
├── demo/knowledge/             # Safe demo runbook
├── docs/                       # Architecture, API, roadmap, and ADRs
├── scripts/                    # Reproducible demo and verification
├── internal/
│   ├── agent/eino/             # ReAct, prompt/parser, tools, and fallback
│   ├── application/chat/       # Chat use case
│   ├── bootstrap/              # Dependency wiring and lifecycle
│   ├── config/                 # Configuration loading and validation
│   ├── eval/                   # Eval-case policy and MySQL store
│   ├── feedback/               # Feedback policy and MySQL store
│   ├── memory/session/         # Redis context and deterministic summary
│   ├── observability/          # Structured logs and OpenTelemetry
│   ├── platform/               # Elasticsearch and MySQL clients
│   ├── retrieval/knowledge/    # Chunking, BM25 policy, and ES store
│   ├── tools/
│   │   ├── common/             # Results, errors, evidence, execution policy
│   │   ├── logs/
│   │   ├── metrics/
│   │   ├── traces/
│   │   └── knowledge/
│   └── transport/http/
│   │   ├── handler/            # Thin Gin handlers
│   │   ├── middleware/         # Gin middleware
│   │   └── dto/                # HTTP request/response types
├── .env.example
├── docker-compose.yml
├── go.mod
└── Makefile
```

Layout rules:

- `cmd` contains no business logic.
- `application` orchestrates use cases; `platform` implements external dependencies.
- Gin remains in the transport layer; handlers delegate immediately to application use cases.
- Eino remains behind the Agent boundary; application and HTTP contracts do not depend on Eino types.
- Tool domain inputs remain separate from backend query languages.
- Do not create `pkg` until a stable API is genuinely reusable outside this project.
- New packages are created only when their implementation begins.

## 8. Configuration

Configuration precedence is defaults < configuration file < environment variables. All values are validated once at startup; invalid settings are never silently corrected at runtime.

Implemented groups:

- `server`: address and HTTP lifecycle timeouts
- `mysql`, `redis`, and `elasticsearch`
- `llm`: provider, endpoint, model, key environment name, temperature, and timeout
- `agent`: mode, maximum iterations, timeout, and prompt version
- `session`: window size, summary threshold, and TTL
- `knowledge`: chunk-size bound
- `telemetry`: service name, environment, OTLP endpoint, and sampling ratio

Secrets enter through environment variables or a secret manager. Example files contain placeholders only.

## 9. Security and Reliability

- Validate upload size, type, content, and decompression behavior.
- Restrict backend queries to allowed services and labels, with maximum time ranges.
- Redact sensitive values from logs, prompts, and trace attributes.
- Apply connect, read, and overall deadlines to every dependency.
- Bound Agent iterations, tool calls, and total execution time.
- Add session and future long-term-memory deletion before production deployment.
- Treat tool output as untrusted model input to resist indirect prompt injection.
- Never present a tool error as normal evidence.

## 10. MVP Acceptance Criteria

- A multi-turn Chat stores recent messages and deterministic rolling summaries in Redis.
- Plain-text or Markdown content is indexed as chunks and retrieved through Elasticsearch BM25.
- Chat can use the bounded Eino ReAct Agent or deterministic fallback through the same interface.
- All four tools expose schemas, bounded execution, structured errors, normalized evidence, and traces.
- Chat returns evidence-aware sections, tool runs, safe limitations, and a trace ID.
- Feedback and manually seeded good/bad eval cases persist in MySQL.
- Local traces can be inspected in Jaeger.
- Docker Compose and scripts reproduce ingestion → Chat → feedback → eval seed.
- `make verify` checks formatting, module stability, tests, vet, and diff whitespace.

## 11. Framework and Technology Choices

WatchOps-Lite uses mature, widely adopted frameworks and libraries for infrastructure and orchestration when they provide reliable building blocks. Features that resemble mature capabilities in the reference project should be implemented with established components rather than custom replacements. This applies to HTTP routing, Agent orchestration, ReAct-style tool calling, tool schema exposure, RAG storage, session caching, durable metadata storage, and tracing infrastructure.

Frameworks remain implementation tools, not the owners of WatchOps-Lite business policy or package boundaries. A custom replacement is justified only when a documented requirement cannot be met through the selected framework.

The accepted decision is recorded in [ADR 0001: Framework and Technology Stack](adr/0001-framework-and-stack.md).

### 11.1 Web Framework: Gin

Gin is the HTTP web framework for WatchOps-Lite. GoFrame will not be used.

Gin is responsible for:

- Routing and route groups
- HTTP middleware
- Request binding
- Validation entry points
- HTTP response formatting

Gin handlers remain thin. They validate and translate transport data, invoke application use cases, and map results into HTTP responses. Business rules, Agent orchestration, retrieval policy, storage decisions, and tool behavior stay outside handlers.

### 11.2 Agent Framework: Eino

Eino is the main Agent and LLM orchestration framework for WatchOps-Lite. The core runtime uses an Eino-based ReAct-style Agent for iterative tool calling and Eino Graph when a workflow benefits from explicit nodes, branches, state transitions, or reusable execution stages.

Eino is responsible for:

- `ChatModel` integration and model invocation
- `PromptTemplate` rendering
- `Graph` construction and graph-style execution
- ReAct-style Agent execution
- Framework-level tool calling

The HTTP and Agent layers have separate responsibilities: Gin owns routing, middleware, request binding, validation entry points, and HTTP responses; Eino owns Agent workflow orchestration, prompt rendering, model invocation, tool calling, and graph-style execution.

WatchOps-Lite still owns its orchestration policy, stop conditions, evidence rules, context budget, prompt content, application boundaries, and domain interfaces. Eino types are adapted at the Agent boundary rather than propagated throughout the domain. No Pilot project structure, prompts, comments, source code, or documentation will be copied.

### 11.3 RAG Stack: Elasticsearch

Elasticsearch is the initial knowledge retrieval backend. Documents are normalized and split into independently addressable chunks before indexing. Each chunk retains its document identity, source location, access metadata, content hash, and update time.

The retrieval roadmap is incremental:

1. Start with a simplified chunk-based BM25 search pipeline.
2. Add embeddings and vector search when the corpus and evaluation cases justify them.
3. Support hybrid lexical and vector retrieval.
4. Fuse ranked result sets with RRF where appropriate.
5. Add reranking behind a replaceable interface.

Elasticsearch-specific clients and query construction stay in the platform adapter. Application and Agent layers depend on WatchOps-Lite retrieval interfaces so ranking strategies can evolve without rewriting Chat orchestration.

### 11.4 Storage

Redis stores short-lived session memory:

- Recent messages in a sliding window
- Rolling structured session summaries
- Optional idempotency keys and short-lived coordination locks

MySQL stores durable business data:

- Long-term memory
- Feedback
- Document metadata and ingestion status
- Eval candidates and review state
- Audit records

Elasticsearch is the knowledge retrieval index, while MySQL is the source of truth for document lifecycle and durable business metadata.

### 11.5 Observability

OpenTelemetry is the tracing standard. Jaeger provides local trace ingestion and visualization. Prometheus metrics are an optional post-MVP enhancement rather than an MVP dependency.

Every important Agent step creates a span, including:

- Context building
- RAG search and ranking
- Tool execution
- Prompt rendering
- Model calls
- Feedback processing

Spans record safe operational metadata such as component name, duration, status, result count, prompt version, model name, and truncation state. Raw user content, retrieved sensitive text, credentials, and unbounded tool output must not be stored as span attributes.

### 11.6 Tool System

Eino owns tool schema exposure, registration, invocation, and integration with the ReAct-style Agent. WatchOps-Lite must not build a custom Tool Registry if Eino already provides the required registration and calling mechanism.

WatchOps-Lite owns:

- Business-level tool input and output contracts
- Structured `ToolError`
- Timeout, bounded retry, and fallback policy
- Evidence normalization
- Sensitive-value redaction
- OpenTelemetry wrappers
- Output size limits and truncation metadata

These policies wrap Eino tools rather than recreating Eino's tool runtime.

Phase 2 introduced deterministic implementations through Eino `InvokableTool` values. The Elasticsearch-backed `search_knowledge` adapter was added in Phase 5, and Phase 8 connects all four tools to a bounded Eino ReAct Agent through Eino's official tool-calling mechanism. Upgrade 1.1 adds Elasticsearch-backed `query_logs` with explicit mock fallback; production metrics and trace connectors remain deferred.

The initial tools are:

- `query_logs`
- `query_metrics`
- `query_traces`
- `search_knowledge`

### 11.7 Local Deployment: Docker Compose

Docker Compose starts Redis, Elasticsearch, MySQL, and Jaeger on their standard local ports. The Go server runs on the host for a short edit-run-debug loop and uses `configs/config.local.json`, copied from the committed example.

The demo keeps the LLM disabled and selects the deterministic Agent by default, so no external key is required. Eino ReAct mode remains opt-in.

Prometheus metrics are a post-MVP enhancement and are not required in the initial Compose stack.

### 11.8 Independence Principle

Use established frameworks for transport, infrastructure, telemetry, and orchestration. In particular, use Gin for the HTTP layer and Eino for the Agent orchestration layer. Do not reinvent mature components unless a documented WatchOps-Lite requirement demands it.

At the same time, WatchOps-Lite keeps its architecture, package structure, prompts, documentation, evaluation policy, and business logic independently designed. It is not a rewrite, fork, or renamed copy of Pilot. It is an original portfolio project that applies related technical ideas to an independently specified Agentic RAG assistant for service reliability analysis.
