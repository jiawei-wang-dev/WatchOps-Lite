# ADR 0010: Elasticsearch-backed Logs Tool

- Status: Accepted
- Date: 2026-07-01

## Context

The MVP `query_logs` tool used deterministic evidence so the Agent, ToolResult contract, partial-failure behavior, and demo could be verified without an observability backend. The next enhancement needs one real operational data source while preserving that stable fallback path.

Logs are the first observability backend because individual events provide direct, inspectable evidence and naturally carry service, timestamp, severity, trace, and span correlation fields. They also complement the existing checkout runbook and error-rate demo without requiring a metrics query language or trace storage model.

## Decision

Use Elasticsearch as the first real logs backend. WatchOps-Lite already operates an Elasticsearch client and local Compose service for knowledge retrieval, so the logs adapter can reuse the connection lifecycle while keeping a separate `watchops_logs` index and mapping.

The logs domain boundary lives under `internal/retrieval/logs` and defines:

- the normalized log event
- bounded search input
- a store interface
- search validation
- the Elasticsearch store

The existing `internal/tools/logs.Input` and Eino tool schema remain unchanged. `query_logs` maps service, RFC3339 time range, keywords, and optional level into the logs search boundary. The configured default limit bounds every request.

## Evidence Mapping

Each Elasticsearch hit becomes a WatchOps `EvidenceItem`:

- `source_type`: `logs`
- `source_name`: `elasticsearch-logs`
- `id`: stable log ID
- `content`: log message
- `resource_id`: service name
- metadata: log ID, level, trace ID, span ID, and timestamp

Log message evidence is rune-bounded and records truncation metadata. The Agent receives only normalized evidence and never constructs raw Elasticsearch JSON.

## Fallback Policy

The default dependency-light configuration keeps `logs.backend=mock`. The full local demo selects Elasticsearch and enables `fallback_to_mock`.

When Elasticsearch is unavailable:

- fallback enabled: return deterministic mock evidence with `LOGS_FALLBACK`, `fallback_used=true`, and backend metadata
- fallback disabled: return `TOOL_DEPENDENCY_UNAVAILABLE`

Index setup failure is logged but does not block application startup. This preserves the MVP resilience contract and keeps tests deterministic.

## Observability

Real searches create:

- `logs.search`
- `elasticsearch.logs.search`

Attributes include backend, index, service, query length, result count, fallback state, and safe error code. Full log messages, credentials, and raw backend errors are excluded from span attributes.

## Why Metrics and Traces Are Deferred

Real metrics require an allowlisted metric/query model and production-safe aggregation semantics. Real traces require a trace backend contract and span-tree normalization. Adding either in this change would mix independent data-source decisions and weaken the evaluation boundary for the logs upgrade.

## Consequences

- Chat can combine real Elasticsearch log evidence with real knowledge evidence.
- Metrics and traces remain deterministic fixtures.
- Elasticsearch now contains independent knowledge and logs indexes.
- The logs seed script uses stable IDs and the Bulk API for repeatable demos.
- Mock fallback remains a deliberate operating mode rather than hidden behavior.
