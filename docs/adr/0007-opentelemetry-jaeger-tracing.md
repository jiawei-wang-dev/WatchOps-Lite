# ADR 0007: OpenTelemetry and Jaeger Tracing

- Status: Accepted
- Date: 2026-07-01

## Context

An Agentic RAG request crosses HTTP transport, session context, Agent routing, tool execution, retrieval, and persistence. A final error or slow response is not enough to explain which stage failed, degraded, timed out, or returned no evidence.

WatchOps-Lite needs one trace context across these boundaries without coupling business services to a Jaeger-specific API.

## Decision

Use the official OpenTelemetry Go API and SDK with the OTLP gRPC trace exporter. Jaeger is the local trace backend because its all-in-one image accepts OTLP and provides a lightweight UI.

The provider records:

- `service.name=watchops-lite`
- configured `deployment.environment`
- parent-based trace-ID ratio sampling

Tracing is disabled by default. Disabled tracing installs a no-op provider. Exporter connection or delivery failure is logged and does not stop HTTP, Chat, retrieval, feedback, or eval behavior.

Gin middleware extracts W3C `traceparent` and `baggage`, creates a server span, places its context on the request, and returns `X-Trace-ID`. Chat returns the active trace ID through its existing response field.

## Span Boundaries

The MVP records:

- `HTTP <method> <route>`
- `chat.execute`
- `session.load_context`
- `session.persist_context`
- `session.update_summary`
- `agent.run`
- `tool.<tool_name>`
- `knowledge.ingest_document`
- `knowledge.chunk_document`
- `knowledge.index_chunks`
- `knowledge.search`
- Elasticsearch adapter operations
- `feedback.create` and `feedback.get`
- `eval.create_case` and `eval.list_cases`

These boundaries separate product policy from dependency calls and make timeout/fallback paths visible.

## Data Safety

Spans may contain bounded operational metadata:

- request, session, document, feedback, and eval case IDs
- tool and case type
- counts, durations, status, summary version, and configured index name
- message or query length, but not content

Spans must not contain:

- user messages or generated answers
- document chunks, retrieved text, raw logs, or tool payloads
- credentials, DSNs, authorization headers, or cookies
- raw Redis, MySQL, Elasticsearch, or model errors

Error events use controlled descriptions. The application logs exporter failures, while API responses continue using sanitized error contracts.

## Why Metrics and Dashboards Are Deferred

Prometheus, Grafana, alerts, SLO dashboards, and advanced sampling require stable metric names, service-level objectives, and production traffic characteristics. This phase establishes trace boundaries first. Adding metrics now would mix signal design with tracing validation and exceed the MVP scope.

## Consequences

- One trace can explain HTTP, context, Agent, tool, and retrieval behavior.
- Domain packages use the vendor-neutral OpenTelemetry API; only provider setup knows OTLP.
- Local Jaeger is optional and never a runtime dependency.
- Batch export adds bounded shutdown work.
- Trace IDs enable manual correlation, but structured log correlation remains future work.

## Future Work

- Prometheus application and Agent metrics
- Grafana dashboards and alerting
- trace-aware structured logging
- tail or adaptive sampling through a production Collector
- deployment-specific resource detection and collector pipelines
