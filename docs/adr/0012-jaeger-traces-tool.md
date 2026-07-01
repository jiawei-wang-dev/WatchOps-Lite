# ADR 0012: Jaeger-backed Traces Tool

- Status: Accepted
- Date: 2026-07-01

## Context

WatchOps-Lite already exports OpenTelemetry traces to Jaeger for local visualization, while the MVP `query_traces` tool still returned deterministic fixtures. Elasticsearch-backed logs and Prometheus-backed metrics made traces the remaining mock observability source.

Distributed trace payloads can be large and vendor-specific. The project needs enough structure to cite slow or error-marked spans without implementing a complete trace analytics engine.

## Decision

Use the Jaeger Query HTTP API as the real `query_traces` backend.

WatchOps-Lite owns a small trace retrieval boundary:

- `internal/retrieval/traces` defines trace query and span models, bounded ranking, and Jaeger response parsing.
- A provided trace ID uses `GET /api/traces/{traceID}`.
- Otherwise the client uses `GET /api/traces` with configured service, operation, start/end microseconds, and limit.
- The parser retains trace/span IDs, parent reference, operation, process service, timing, simple tags, and error status.
- The service ranks error-marked spans first and then duration descending.
- The tool maps selected spans to normalized evidence with `source_type=traces`, `source_name=jaeger`, trace resource identity, and bounded metadata.
- Raw Jaeger responses, logs, and full tag collections are not exposed as evidence or telemetry attributes.

Exact trace-ID lookup treats the trace ID as authoritative and does not discard spans when an Agent-provided service hint differs from Jaeger's process service.

When Jaeger is unavailable and fallback is enabled, `query_traces` returns its deterministic evidence with a `TRACES_FALLBACK` warning and `fallback_used=true`. With fallback disabled, it returns `TOOL_DEPENDENCY_UNAVAILABLE`. No matching spans produce `TRACES_NO_DATA`; the tool does not invent evidence.

## Consequences

The local demo can now correlate four real evidence categories:

- Prometheus metrics
- Elasticsearch logs
- Elasticsearch knowledge
- Jaeger traces

The same Jaeger instance serves both local trace visualization and read-only Agent retrieval. Jaeger availability is checked at tool execution time and never blocks application startup.

Complex critical-path reconstruction, service dependency graphs, trace anomaly detection, sampling analysis, and cross-trace aggregation remain deferred. The first implementation intentionally returns a bounded list of relevant spans.

Mock fallback remains useful for unit tests, dependency-light startup, and degraded environments, but it remains explicitly labeled and cannot impersonate Jaeger evidence.

## Independence

This design is independently implemented for WatchOps-Lite. It does not copy Pilot project structure, prompts, comments, source code, or documentation.
