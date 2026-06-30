# Project Structure

WatchOps-Lite keeps its repository structure intentionally small. A package is created only when it contains an implemented responsibility; planned architecture alone is not a reason to add empty directories.

## Current Structure

```text
.
├── cmd/server/                  # Process entry point
├── configs/                    # Example runtime configuration
├── docs/                       # Product and architecture documentation
└── internal/
    ├── bootstrap/              # Dependency wiring and server lifecycle
    ├── config/                 # Configuration loading and validation
    ├── observability/          # Structured logging and OTel lifecycle boundary
    ├── agent/eino/             # Eino assembly for current mock tools
    ├── application/chat/       # Chat use-case validation and orchestration
    ├── memory/session/
    │   ├── redisstore/         # Redis list/hash persistence and TTL
    │   └── summary/            # Deterministic rolling summarizer
    ├── platform/elasticsearch/ # Official client configuration and request boundary
    ├── retrieval/knowledge/    # Chunking, retrieval service, and ES-backed store
    ├── tools/
    │   ├── common/             # Shared results, errors, evidence, execution policy
    │   ├── logs/               # Deterministic query_logs mock
    │   ├── metrics/            # Deterministic query_metrics mock
    │   ├── traces/             # Deterministic query_traces mock
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
- `internal/observability` owns structured logging and the future OpenTelemetry SDK boundary.
- `internal/agent/eino` exposes typed mock implementations through Eino's official Tool abstraction.
- `internal/agent/eino` also contains the deterministic Phase 3 runner; full Graph/ReAct orchestration remains deferred.
- `internal/application/chat` owns the Chat use case and does not depend on Gin.
- `internal/memory/session` owns short-term memory contracts.
- `internal/memory/session/redisstore` owns Redis key and transaction behavior.
- `internal/memory/session/summary` owns deterministic rolling summarization.
- `internal/platform/elasticsearch` owns official-client construction and bounded requests.
- `internal/retrieval/knowledge` owns document/chunk models, deterministic chunking, retrieval policy, and Elasticsearch query construction.
- `internal/tools` owns WatchOps business contracts, normalized evidence, structured errors, timeout policy, and mock implementations.
- `internal/transport/http` contains HTTP concerns only: Gin routing, middleware, handlers, and later transport DTOs.
- Gin handlers bind and validate HTTP input, call application-level operations, and format HTTP output. Business rules do not belong in handlers.

## Intended Future Modules

The following modules are part of the planned architecture, but their directories will be added only when their first implementation is introduced:

| Module | Intended responsibility |
| --- | --- |
| `internal/feedback` | Likes, dislikes, eval candidates, review flow, and evaluation reuse |
| Additional `internal/platform` packages | MySQL, model-provider, and other external-system adapters |

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
