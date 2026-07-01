# Project Structure

WatchOps-Lite keeps its repository structure intentionally small. A package is created only when it contains an implemented responsibility; planned architecture alone is not a reason to add empty directories.

## Current Structure

```text
.
├── cmd/
│   ├── server/                 # Application process entry point
│   └── demo-metrics/           # Local Prometheus scrape target
├── configs/                    # App configuration and Prometheus scrape config
├── demo/                       # Safe runbook and deterministic log fixtures
├── docs/                       # Product and architecture documentation
├── scripts/                    # Demo flow and verification gate
├── docker-compose.yml          # Redis, Elasticsearch, Prometheus, MySQL, and Jaeger
└── internal/
    ├── bootstrap/              # Dependency wiring and server lifecycle
    ├── config/                 # Configuration loading and validation
    ├── observability/          # Structured logging and OTLP tracing lifecycle
    ├── agent/eino/             # ReAct Graph, PromptTemplate, output parser, tools, fallback
    ├── application/chat/       # Chat use-case validation and orchestration
    ├── memory/session/
    │   ├── redisstore/         # Redis list/hash persistence and TTL
    │   └── summary/            # Deterministic rolling summarizer
    ├── feedback/               # Feedback validation, store contract, and MySQL store
    ├── eval/                   # Eval-seed validation, store contract, and MySQL store
    ├── platform/elasticsearch/ # Official client configuration and request boundary
    ├── platform/mysql/         # database/sql pool and feedback/eval schema
    ├── retrieval/knowledge/    # Chunking, retrieval service, and ES-backed store
    ├── retrieval/embedding/    # Optional deterministic/test and OpenAI-compatible embeddings
    ├── retrieval/logs/         # Bounded log search and Elasticsearch store
    ├── retrieval/metrics/      # Allowlisted queries and Prometheus HTTP client
    ├── retrieval/traces/       # Bounded trace search and Jaeger Query API client
    ├── tools/
    │   ├── common/             # Shared results, errors, evidence, execution policy
    │   ├── logs/               # Elasticsearch query_logs with mock fallback
    │   ├── metrics/            # Prometheus query_metrics with mock fallback
    │   ├── traces/             # Jaeger query_traces with mock fallback
    │   └── knowledge/          # Elasticsearch search tool with mock fallback
    └── transport/http/
        ├── handler/            # Thin Gin handlers
        ├── middleware/         # Gin request middleware
        └── router.go           # Routes and middleware registration
```

### Current Boundaries

- `cmd/server` parses process-level inputs, initializes dependencies, and starts the application.
- `internal/bootstrap` wires dependencies and owns HTTP server startup and graceful shutdown.
- `internal/config` loads defaults, JSON configuration, and environment overrides.
- `internal/observability` owns structured logging, the OpenTelemetry provider lifecycle, OTLP export, and safe tracing helpers.
- `internal/agent/eino` owns Eino ReAct Graph orchestration, the versioned prompt, structured output parsing, Eino tool adaptation, model/tool tracing, and deterministic fallback.
- The deterministic runner remains the default test/local path and the request-time fallback for unavailable LLM calls.
- `internal/application/chat` owns the Chat use case and does not depend on Gin.
- `internal/memory/session` owns short-term memory contracts.
- `internal/memory/session/redisstore` owns Redis key and transaction behavior.
- `internal/memory/session/summary` owns deterministic rolling summarization.
- `internal/platform/elasticsearch` owns official-client construction and bounded requests.
- `internal/retrieval/embedding` owns the optional provider boundary and vector generation.
- `internal/retrieval/knowledge` owns document/chunk models, deterministic chunking, BM25/vector policy, RRF fusion, fallback, and Elasticsearch query construction.
- `internal/retrieval/logs` owns log-event models, bounded search policy, and Elasticsearch query construction.
- `internal/retrieval/metrics` owns metric samples, allowlisted query selection, and Prometheus response parsing.
- `internal/retrieval/traces` owns trace/span models, bounded ranking, and Jaeger response parsing.
- `internal/feedback` owns feedback validation and persistence contracts.
- `internal/eval` owns manual good-case/bad-case seeding and feedback-rating compatibility.
- `internal/platform/mysql` owns MySQL driver, connection-pool settings, and schema initialization.
- `internal/tools` owns WatchOps business contracts, normalized evidence, structured errors, timeout policy, real adapters, and deterministic fallbacks.
- `internal/transport/http` contains HTTP concerns only: Gin routing, middleware, handlers, and later transport DTOs.
- Gin handlers bind and validate HTTP input, call application-level operations, and format HTTP output. Business rules do not belong in handlers.

## Intended Future Modules

The following modules are part of the planned architecture, but their directories will be added only when their first implementation is introduced:

| Module | Intended responsibility |
| --- | --- |
| Feedback review/export | Redaction, approval state, and `agent_eval_cases.json` export |
| Additional `internal/platform` packages | Model-provider and other external-system adapters |

These names describe ownership boundaries, not a requirement to create every package at once.

## Dependency Direction

Future features should follow this direction:

```text
HTTP transport -> application/use case -> domain contract
                                      <- platform adapter
```

Agent orchestration uses Eino, including Eino's Tool registration and calling mechanism. WatchOps-Lite retains its business contracts and does not create a competing Tool Registry. Elasticsearch, Redis, MySQL, and other vendor clients remain in platform adapters and do not leak into HTTP handlers or core policy.

## Growth Rule

When adding a feature:

1. Create only the packages needed for that feature.
2. Keep transport types at the transport boundary.
3. Define domain contracts before binding orchestration to infrastructure.
4. Put vendor-specific integration code under `internal/platform`.
5. Avoid catch-all packages such as `utils`, `common`, or a premature public `pkg`.
