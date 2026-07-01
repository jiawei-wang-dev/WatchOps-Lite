# ADR 0011: Prometheus-backed Metrics Tool

- Status: Accepted
- Date: 2026-07-01

## Context

The MVP `query_metrics` tool returned deterministic fixtures. Upgrade 1.1 introduced real Elasticsearch log evidence, leaving metrics as the next observability source required for a credible checkout reliability investigation.

Metrics systems have specialized query, labeling, and time-series semantics. WatchOps-Lite should use a mature backend rather than implement custom time-series storage. It must also remain runnable for tests, portfolio demos, and degraded environments where that backend is absent.

## Decision

Use the Prometheus HTTP API as the real `query_metrics` backend.

WatchOps-Lite owns a small metrics boundary:

- `internal/retrieval/metrics` defines metric samples, configured-query selection, and a Prometheus client.
- Configuration maps business query names to allowlisted PromQL expressions.
- Agents provide domain inputs such as service, symptom, metric name, and time range; they cannot submit arbitrary PromQL.
- The initial adapter performs an instant query at the end of the requested interval.
- Prometheus vector samples become `ToolResult` evidence with `source_type=metrics`, `source_name=prometheus`, the service resource ID, and metric name, value, labels, query, and timestamp metadata.
- `metrics.query` and `prometheus.query` spans record bounded operational attributes without response bodies or sensitive values.

The local Compose stack runs a small Go demo exporter. Prometheus scrapes four deterministic checkout signals:

- checkout error rate
- checkout p95 latency
- payment dependency latency
- checkout timeout count

When Prometheus is unavailable and fallback is enabled, `query_metrics` returns its existing deterministic evidence with a `METRICS_FALLBACK` warning and `fallback_used=true`. With fallback disabled, it returns `TOOL_DEPENDENCY_UNAVAILABLE`. Prometheus availability never blocks application startup.

## Consequences

The demo can correlate real Prometheus metric evidence with real Elasticsearch log and knowledge evidence while retaining deterministic operation without an LLM.

Configured expressions keep query behavior auditable and prevent an Agent from issuing expensive or unsafe arbitrary PromQL. The first implementation does not expose the full Prometheus query language and uses instant rather than range queries.

Mock fallback remains valuable for unit tests, dependency-light startup, and degraded demonstrations. It remains explicitly labeled and cannot impersonate Prometheus evidence.

Real trace retrieval is deferred so this upgrade stays focused on metrics. Grafana dashboards and Prometheus instrumentation of WatchOps-Lite itself are also deferred; neither is required to prove the metrics evidence path.

## Independence

This design is independently implemented for WatchOps-Lite. It does not copy Pilot project structure, prompts, comments, source code, or documentation.
