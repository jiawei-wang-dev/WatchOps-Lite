# ADR 0016: Runtime Prometheus Metrics

## Status

Accepted

## Context

WatchOps-Lite already queries Prometheus through `query_metrics`, but that integration observes the services being investigated. It does not describe the health or performance of WatchOps-Lite itself. OpenTelemetry traces provide request-level causality, while operators also need aggregate rates, error counts, fallback frequency, and latency distributions.

## Decision

WatchOps-Lite exposes an optional `GET /metrics` endpoint using the official Prometheus Go client and a private registry. Runtime instrumentation records:

- HTTP and Chat request counts and latency
- tool calls, latency, and structured errors
- knowledge RAG search latency
- unavailable session memory
- Agent and LLM-summary fallbacks
- rule-based eval run outcomes

HTTP route labels use Gin's route template rather than raw paths to limit cardinality. Tool names, structured error codes, retrieval modes, and bounded status values are allowed labels. User messages, prompts, model output, evidence content, session IDs, request IDs, and trace IDs are never metric labels.

Collection and endpoint registration are controlled by `runtime_metrics.enabled` or `WATCHOPS_RUNTIME_METRICS_ENABLED`. The local Prometheus configuration scrapes WatchOps-Lite independently of the demo exporter that supplies evidence to `query_metrics`.

## Consequences

Prometheus metrics complement, rather than replace, OpenTelemetry tracing. Metrics answer aggregate questions such as rate, latency, and fallback frequency; traces explain an individual Agent run.

The private registry prevents duplicate global registration during tests and embedded application startup. Disabled mode is a no-op and does not register `/metrics`.

Grafana is deferred so this stage remains focused on trustworthy instrumentation and scrape behavior. A later stage can visualize these stable metric names without changing core business logic.
