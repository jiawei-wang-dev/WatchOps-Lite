# ADR 0009: MVP Demo Packaging

- Status: Accepted
- Date: 2026-07-01

## Context

Phases 1 through 8 established the HTTP, Agent, context, retrieval, feedback, and tracing capabilities independently. The implementation is testable, but a portfolio reader should not need to reverse-engineer dependency versions, configuration switches, seed data, or API ordering to see the complete system.

The final MVP phase therefore prioritizes a reproducible local demonstration rather than another backend capability.

## Decision

Provide a local package consisting of:

- `docker-compose.yml` for Redis, Elasticsearch, MySQL, and Jaeger
- a committed full-demo configuration example and an ignored local copy
- a safe checkout-service runbook
- scripts for knowledge ingestion, Chat, feedback, and eval-case creation
- a combined local verification script
- documentation that separates implemented behavior from deferred work

The Go server runs on the host instead of in Compose. This keeps the normal Go edit, test, and debugger workflow fast while Compose owns only stateful and visualization dependencies.

## Why Docker Compose

The demo crosses four external systems with different startup conventions. Compose gives contributors one declarative command, stable local ports, health checks for the stateful dependencies, named volumes, and a single cleanup boundary. It is a development environment, not a production deployment specification.

Elasticsearch security is disabled only for this loopback-oriented local demo. The Compose credentials are demonstration values and must not be reused outside local development.

## Why the LLM Is Disabled by Default

An API key would make the first run provider-dependent, billable, and nondeterministic. The default configuration therefore selects the deterministic runner and requires no external model account. Contributors can still enable the Eino ReAct runner explicitly with an OpenAI-compatible endpoint, model name, and environment-provided key.

## Why Deterministic Fallback Remains Important

The deterministic runner makes the architecture demonstrable when model configuration is missing or a model request fails. It also gives tests a stable execution path and makes tool, evidence, memory, retrieval, feedback, and tracing behavior inspectable without attributing every failure to a remote model.

Fallback does not pretend to be production reasoning. Responses identify deterministic/mock evidence and surface model fallback as a limitation.

## Demonstrated Loop

The scripted sequence proves the MVP integration path:

```text
runbook -> Elasticsearch chunks -> Chat
        -> Redis context -> Agent tool calls -> evidence/tool_runs
        -> trace_id -> Jaeger
        -> MySQL feedback -> manual eval case
```

Runtime response IDs are stored under `/tmp` so feedback and eval cases preserve provenance without requiring jq or committing generated files.

## Deferred Work

This packaging phase does not add:

- embeddings, hybrid retrieval, RRF, or reranking
- LLM session summarization
- production logs, metrics, or trace adapters
- automatic eval execution or an LLM judge
- Prometheus application metrics or Grafana dashboards
- production container hardening, secrets management, or orchestration

These features should be introduced only with their own requirements, evaluation criteria, and ADRs.

## Consequences

- A new contributor can reproduce the major MVP paths from documented commands.
- The default demo remains free of external LLM credentials.
- Local state persists across Compose restarts unless volumes are intentionally removed.
- The repository now has packaging assets that require version maintenance.
- Production deployment remains explicitly out of scope.
