# ADR 0017: Grafana Dashboard

## Status

Accepted

## Context

Stage 4 exposed WatchOps-Lite runtime metrics and configured Prometheus scraping. Raw PromQL proves collection, but it is a poor demonstration surface for the relationship between HTTP traffic, Agent work, tool errors, retrieval latency, degraded memory, fallbacks, and eval activity.

## Decision

The local Docker Compose stack includes a pinned Grafana service. Provisioning files create:

- a read-only Prometheus datasource using the Compose service address
- a stable `watchops-lite` dashboard
- panels for HTTP rate and latency, Chat count and latency, tool calls and errors, RAG latency, session-memory unavailability, Agent and summary fallbacks, and eval run status

Grafana listens only on `127.0.0.1:3000`. Anonymous Viewer access removes login setup from the local portfolio demo; this setting is not intended as a production authentication policy. Dashboard and datasource definitions are version-controlled and mounted read-only.

## Consequences

The dashboard makes Stage 4 instrumentation visible without adding backend features or changing business logic. It also keeps Prometheus's two roles clear: the demo service metrics are Agent evidence, while WatchOps-Lite runtime metrics feed the dashboard.

This is deliberately a starter dashboard, not a production SRE dashboard. Alert rules, recording rules, capacity views, SLOs, tenant filters, retention policy, authentication integration, and operational ownership are deferred.
