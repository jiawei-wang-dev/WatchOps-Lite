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

A user uploads a Markdown, plain-text, or PDF runbook. The system extracts, chunks, embeds, and indexes its content. Later, `search_knowledge` returns quoted excerpts with source locations.

### C. Follow-up Questions

A user asks, “How did we resolve a similar issue last time?” The system combines recent Redis messages and the session summary with relevant, confirmed long-term memories from MySQL.

### D. Feedback Reuse

A liked, evidence-rich answer becomes a positive eval candidate. A disliked answer with a reason becomes a bad-case candidate. After redaction and review, approved cases are added to `agent_eval_cases.json` for regression testing.

## 3. Scope and Boundaries

### 3.1 Included in the MVP

- Go HTTP server and versioned REST API
- Chat requests, with synchronous responses first and streaming reserved
- RAG document upload, status, and search
- Logs, metrics, traces, and knowledge-search tools
- Redis session summaries and recent messages
- MySQL long-term memory, document metadata, feedback, and eval candidates
- OpenTelemetry traces, basic metrics, and structured logging
- JSON evaluation data and a CLI evaluation entry point

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

### 4.3 Harness Engineering

All tools run through a shared `Tool` interface and registry:

```go
type Tool interface {
    Name() string
    Validate(input json.RawMessage) error
    Execute(ctx context.Context, input json.RawMessage) (ToolResult, error)
}
```

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

Responsibilities: request validation, session loading, Agent execution, evidence assembly, context persistence, and response delivery.

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
- `GET /api/v1/knowledge/documents/{id}`: inspect processing status
- `POST /api/v1/knowledge/search`: perform explicit retrieval
- `DELETE /api/v1/knowledge/documents/{id}`: logically delete and asynchronously remove an index

Processing stages: validate → extract → normalize → chunk → embed → vector upsert → update status.

Each chunk records its `document_id`, title, section position, content hash, source, access scope, and update time. Every retrieval result must locate its original source.

### 5.3 Tools

| Tool | Purpose | Primary constraints | Suggested adapter |
| --- | --- | --- | --- |
| `query_logs` | Search and aggregate logs | Bounded time range, result count, and labels | Loki HTTP API |
| `query_metrics` | Query time-series metrics | Allowlisted metrics and functions | Prometheus HTTP API |
| `query_traces` | Find traces and important spans | Bounded services, time range, and result count | Tempo HTTP API |
| `search_knowledge` | Retrieve runbooks and postmortems | Top-k, score threshold, and access filters | `VectorStore` |

Tools are read-only by default. The model produces constrained domain parameters, and adapters construct backend queries. Arbitrary LogQL or PromQL must not pass directly from the model to a backend.

### 5.4 Memory and Feedback

Redis stores:

- Session summaries
- Recent messages
- Optional idempotency keys and short-lived locks

MySQL stores:

- `sessions`
- `memories`
- `knowledge_documents`
- `feedback`
- `eval_candidates`

The vector index stores only vectors and filtering metadata required for retrieval. MySQL remains the source of truth for document state and audit information.

## 6. Common API Conventions

- Prefix: `/api/v1`
- JSON fields: `snake_case`
- Time: UTC RFC 3339
- IDs: ULID or UUIDv7
- Request correlation: accept or generate `X-Request-ID`
- Idempotent upload: support `Idempotency-Key`
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

Initial error codes: `INVALID_ARGUMENT`, `UNAUTHORIZED`, `NOT_FOUND`, `CONFLICT`, `RATE_LIMITED`, `DEPENDENCY_UNAVAILABLE`, `TOOL_TIMEOUT`, and `INTERNAL`.

## 7. Recommended Go Project Layout

```text
WatchOps-Lite/
├── cmd/
│   ├── server/                 # HTTP entry point; wiring and startup only
│   └── eval/                   # Offline Agent evaluation command
├── api/
│   └── openapi/                # OpenAPI contract
├── configs/                    # Local sample configuration; no secrets
├── deployments/
│   ├── docker/                 # Container build assets
│   └── otel/                   # Local Collector configuration
├── docs/
├── evals/
│   ├── agent_eval_cases.json
│   └── fixtures/               # Redacted, deterministic tool responses
├── internal/
│   ├── bootstrap/              # Dependency wiring and lifecycle
│   ├── config/                 # Configuration loading and validation
│   ├── domain/                 # Entities, value objects, and ports
│   ├── application/
│   │   ├── chat/               # Chat use cases
│   │   ├── knowledge/          # Document and retrieval use cases
│   │   └── feedback/           # Feedback use cases
│   ├── agent/
│   │   ├── orchestrator/       # Single-Agent state machine and stop rules
│   │   ├── prompt/             # Versioned templates and rendering
│   │   ├── context/            # Summary, window, and token budget
│   │   ├── harness/            # Timeout, fallback, and structured errors
│   │   └── loop/               # Feedback-to-eval candidates
│   ├── tools/
│   │   ├── registry/
│   │   ├── logs/
│   │   ├── metrics/
│   │   ├── traces/
│   │   └── knowledge/
│   ├── retrieval/
│   │   ├── chunker/
│   │   ├── embedding/
│   │   └── vectorstore/
│   ├── memory/
│   │   ├── session/            # Redis implementation
│   │   └── longterm/           # MySQL implementation
│   ├── transport/http/
│   │   ├── handler/
│   │   ├── middleware/
│   │   └── dto/
│   ├── platform/
│   │   ├── mysql/
│   │   ├── redis/
│   │   └── httpclient/
│   └── observability/          # OTel, logs, and metrics
├── migrations/                 # MySQL migrations
├── test/
│   ├── integration/
│   └── contract/
├── .env.example
├── compose.yaml
├── go.mod
└── Makefile
```

Layout rules:

- `cmd` contains no business logic.
- `internal/domain` does not import database, HTTP, or model-vendor SDKs.
- `application` orchestrates use cases; `platform` implements external dependencies.
- Tool domain inputs remain separate from backend query languages.
- Do not create `pkg` until a stable API is genuinely reusable outside this project.
- Prompt templates and eval fixtures are product assets and require version review.

## 8. Configuration

Configuration precedence is defaults < configuration file < environment variables. All values are validated once at startup; invalid settings are never silently corrected at runtime.

Recommended groups:

- `server`: address, timeouts, and body limits
- `mysql`, `redis`, and `vector_store`
- `llm` and `embedding`
- `tools.logs`, `tools.metrics`, and `tools.traces`
- `agent`: maximum steps, token budget, and tool concurrency
- `session`: window size, summary threshold, and TTL
- `telemetry`: service name, OTLP endpoint, and sampling ratio

Secrets enter through environment variables or a secret manager. Example files contain placeholders only.

## 9. Security and Reliability

- Validate upload size, type, content, and decompression behavior.
- Restrict backend queries to allowed services and labels, with maximum time ranges.
- Redact sensitive values from logs, prompts, and trace attributes.
- Apply connect, read, and overall deadlines to every dependency.
- Bound Agent iterations, tool calls, and total execution time.
- Let users delete sessions and long-term memory, with auditable deletion.
- Treat tool output as untrusted model input to resist indirect prompt injection.
- Never present a tool error as normal evidence.

## 10. MVP Acceptance Criteria

- A multi-turn Chat stores observable summaries and recent messages in Redis.
- A document can be uploaded and retrieved through `search_knowledge` with source locations.
- All four tools enforce schema validation, timeouts, and structured errors.
- If one logs, metrics, or traces backend fails, the answer still presents available evidence and limitations.
- A Chat root span contains model, context, retrieval, and tool child spans.
- Likes and dislikes persist and can be exported as redacted eval candidates.
- `agent_eval_cases.json` runs through a CLI and emits a machine-readable report.
- Critical domain and use-case logic has unit tests; external adapters have contract tests.
