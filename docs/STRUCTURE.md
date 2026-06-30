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
- `internal/transport/http` contains HTTP concerns only: Gin routing, middleware, handlers, and later transport DTOs.
- Gin handlers bind and validate HTTP input, call application-level operations, and format HTTP output. Business rules do not belong in handlers.

## Intended Future Modules

The following modules are part of the planned architecture, but their directories will be added only when their first implementation is introduced:

| Module | Intended responsibility |
| --- | --- |
| `internal/agent` | Eino-based Graph and ReAct orchestration, prompt rendering, model invocation, context assembly, and stop conditions |
| `internal/tools` | Business-level tool I/O contracts, `ToolError`, execution policy wrappers, evidence normalization, redaction, and Eino Tool implementations |
| `internal/retrieval` | Document chunking, retrieval contracts, ranking policy, and Elasticsearch-backed knowledge search |
| `internal/memory` | Session-memory and long-term-memory contracts and application-facing behavior |
| `internal/feedback` | Likes, dislikes, eval candidates, review flow, and evaluation reuse |
| `internal/platform` | Infrastructure adapters for Elasticsearch, Redis, MySQL, model providers, and other external systems |

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
