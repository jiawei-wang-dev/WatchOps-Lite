# WatchOps-Lite

WatchOps-Lite is an Agentic RAG assistant for service reliability analysis. It retrieves operational knowledge, queries logs, metrics, and traces, and produces evidence-backed findings.

> Current status: design phase. The repository does not yet contain the full business implementation.

## Goals

- Provide a unified Chat API for incident analysis.
- Upload, chunk, index, and retrieve operational knowledge through RAG.
- Let the Agent invoke `query_logs`, `query_metrics`, `query_traces`, and `search_knowledge` when needed.
- Store session summaries and recent messages in Redis, and long-term memory and feedback in MySQL.
- Trace model, retrieval, and tool activity with OpenTelemetry.
- Turn positive and negative feedback into reusable regression evaluation cases.

## Four Agent Engineering Disciplines

| Discipline | WatchOps-Lite implementation |
| --- | --- |
| Prompt Engineering | Versioned output templates, evidence-bound claims, and explicit handling of insufficient information |
| Context Engineering | Redis session summaries, a sliding message window, layered context, pruning, and token budgets |
| Harness Engineering | Tool validation, timeouts, bounded retries, fallbacks, and structured errors |
| Loop Engineering | Positive cases from likes, bad cases from dislikes, human review, and offline eval reuse |

## MVP Scope

- Go HTTP server
- Chat API
- RAG document upload and search
- Four reliability-analysis tools
- Redis short-term session context
- MySQL long-term memory
- User feedback loop
- OpenTelemetry tracing
- Offline `agent_eval_cases.json`

The recommended infrastructure is MySQL, Redis, a vector database implementing the `VectorStore` interface, and observability backends compatible with the Loki, Prometheus, and Tempo APIs. Vendor choices stay outside the domain layer.

## Design Documents

- [Project blueprint](docs/PROJECT_BLUEPRINT.md)
- [System architecture](docs/ARCHITECTURE.md)
- [Development roadmap](docs/ROADMAP.md)

## Planned Local Development

After the scaffolding phase, the intended developer experience is:

```bash
cp .env.example .env
docker compose up -d
go run ./cmd/server
```

The planned default endpoints are:

```text
GET  http://localhost:8080/healthz
POST http://localhost:8080/api/v1/chat
```

These commands describe the target state and are not runnable during the current design phase.

## Design Principles

1. Evidence first: distinguish observed facts, inferences, and recommendations.
2. Clear boundaries: domain logic does not depend on HTTP, databases, or model SDKs.
3. Safe by default: tools are read-only, inputs are bounded, and sensitive values are redacted.
4. Replaceable integrations: model, vector store, and observability providers connect through ports.
5. Evaluation-ready: production feedback becomes stable offline regression material.

## Non-goals

- Directly executing production changes, deployments, scaling, or deletion.
- Replacing alerting platforms, incident command processes, or human approval.
- Copying source code, layouts, prompts, comments, or documentation from training or other projects.
- Multi-agent collaboration and complex workflow orchestration in the MVP.

## License

Apache-2.0 is planned. A `LICENSE` file will be added before the first release.
