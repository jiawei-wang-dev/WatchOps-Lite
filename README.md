# WatchOps-Lite

WatchOps-Lite is an Agentic RAG assistant for service reliability analysis. Its long-term goal is to combine operational knowledge with logs, metrics, and traces, then produce evidence-backed findings for on-call engineers and SRE teams.

The project currently contains the Gin HTTP API, an optional Eino ReAct Agent with deterministic fallback, Redis session memory, Elasticsearch-backed BM25 knowledge retrieval, MySQL-backed feedback/eval seeding, and OpenTelemetry tracing.

## Current Scope

Implemented:

- Gin HTTP server with graceful shutdown
- `GET /healthz`
- JSON configuration with environment-variable overrides
- JSON structured logging with request metadata
- OpenTelemetry tracing with OTLP gRPC export and graceful shutdown
- Request trace propagation through Chat, session memory, Agent, tools, RAG, feedback, and eval
- Unit tests for configuration and health handling
- Shared `ToolResult`, `ToolError`, and evidence contracts
- Timeout and fallback execution wrapper
- Eino-backed mock tools for logs, metrics, traces, and knowledge
- `POST /api/v1/chat` with configurable Eino ReAct or deterministic execution
- Versioned Eino PromptTemplate and OpenAI-compatible ChatModel integration
- Official Eino ReAct Graph tool calling over the four existing tools
- Evidence-ID validation and safe structured-output parsing
- Startup and per-request deterministic fallback when the LLM is unavailable
- Evidence-aware answers and tool run summaries
- Redis recent messages, versioned rolling summaries, and graceful memory degradation
- Plain-text/Markdown knowledge ingestion with deterministic paragraph-first chunking
- Elasticsearch chunk indexing and BM25 knowledge search
- Real `search_knowledge` evidence with explicit mock fallback when Elasticsearch is unavailable
- Upvote/downvote feedback persistence with answer and evidence snapshots
- Manual good-case and bad-case eval seeding from reviewed feedback

Not implemented yet:

- Real logs, metrics, or traces backends
- Embeddings, vector/hybrid retrieval, reranking, and RRF
- Automatic eval runner, LLM judge, scoring, and prompt comparison
- MySQL long-term memory, audit records, and document metadata

## Requirements

- Go 1.23 or newer
- `make` for the convenience commands

The HTTP server can start without external services. Redis is required for session continuity, but a Redis outage does not fail Chat requests. Elasticsearch and MySQL are disabled by default. Disabled persistence endpoints return a structured `503` response.

OpenTelemetry is also disabled by default. When enabled, an unavailable collector does not block business requests; export failures are logged.

## Run Locally

Start the server with the default configuration:

```bash
make run
```

Or run it directly:

```bash
go run ./cmd/server -config configs/config.json
```

Check the health endpoint:

```bash
curl -i http://localhost:8080/healthz
```

Call the Chat endpoint:

```bash
curl -sS http://localhost:8080/api/v1/chat \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "ses_01",
    "message": "Why did checkout error rate increase in the last 20 minutes?",
    "time_context": {
      "from": "2026-06-30T00:00:00Z",
      "to": "2026-06-30T00:20:00Z"
    }
  }'
```

The default configuration uses the deterministic runner and mock observability tools. See [HTTP API](docs/API.md) for the request, response, Agent modes, and error contracts.

Redis defaults to `localhost:6379`. Configure it through `configs/config.json` or `WATCHOPS_REDIS_*` environment variables. Session defaults are a 12-message window, a 12-message summary threshold, and a 24-hour TTL.

To use real knowledge retrieval, run Elasticsearch 9.x locally and enable it:

```bash
export WATCHOPS_ELASTICSEARCH_ENABLED=true
export WATCHOPS_ELASTICSEARCH_ADDRESSES=http://localhost:9200
make run
```

Ingest and search a runbook:

```bash
curl -sS http://localhost:8080/api/v1/knowledge/documents \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "Checkout runbook",
    "source": "manual",
    "content": "Check upstream timeout saturation.\n\nCompare retry volume with latency.",
    "metadata": {"service": "checkout"}
  }'

curl -sS http://localhost:8080/api/v1/knowledge/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"checkout timeout","limit":5}'
```

To persist feedback and eval seeds, create a MySQL database and enable the integration:

```bash
mysql -u root -p -e "
  CREATE DATABASE IF NOT EXISTS watchops_lite CHARACTER SET utf8mb4;
  CREATE USER IF NOT EXISTS 'watchops'@'%' IDENTIFIED BY 'watchops';
  GRANT ALL PRIVILEGES ON watchops_lite.* TO 'watchops'@'%';
"
export WATCHOPS_MYSQL_ENABLED=true
export 'WATCHOPS_MYSQL_DSN=watchops:watchops@tcp(localhost:3306)/watchops_lite?parseTime=true'
make run
```

WatchOps-Lite creates the minimal `feedback` and `eval_cases` tables when MySQL is reachable. Submit a downvote and seed a bad case:

```bash
curl -sS http://localhost:8080/api/v1/feedback \
  -H 'Content-Type: application/json' \
  -d '{
    "request_id":"req-123",
    "session_id":"ses_01",
    "rating":"down",
    "reason_tags":["missing_evidence"],
    "comment":"The answer did not cite trace evidence."
  }'

curl -sS http://localhost:8080/api/v1/eval/cases \
  -H 'Content-Type: application/json' \
  -d '{
    "feedback_id":"fb_replace_me",
    "case_type":"bad_case",
    "input_message":"Why did checkout error rate increase?",
    "expected_behavior":"Cite evidence and report missing trace data."
  }'
```

Example response:

```json
{
  "status": "ok",
  "service": "watchops-lite",
  "time": "2026-06-30T00:00:00Z"
}
```

Stop the server with `Ctrl+C`. It handles `SIGINT` and `SIGTERM` with a bounded graceful shutdown.

## Enable the Eino ReAct Agent

WatchOps-Lite supports OpenAI-compatible chat-completion providers through Eino:

```bash
export WATCHOPS_AGENT_MODE=eino_react
export WATCHOPS_AGENT_MAX_ITERATIONS=6
export WATCHOPS_AGENT_PROMPT_VERSION=watchops_agent_v1
export WATCHOPS_LLM_ENABLED=true
export WATCHOPS_LLM_BASE_URL=https://api.openai.com/v1
export WATCHOPS_LLM_MODEL=your-tool-calling-model
export WATCHOPS_LLM_API_KEY=replace-me
make run
```

`WATCHOPS_LLM_API_KEY_ENV` defaults to `WATCHOPS_LLM_API_KEY`; configuration stores only the environment-variable name, never the secret.

If LLM configuration or initialization is incomplete, startup selects the deterministic runner. If an LLM request fails later, that Chat request falls back to the deterministic runner and includes `AGENT_LLM_FALLBACK` plus `metadata.fallback_used=true`. Malformed model JSON does not become an unchecked answer: the response contains `AGENT_OUTPUT_PARSE_FAILED`.

## Local Tracing with Jaeger

Run Jaeger all-in-one with OTLP enabled:

```bash
docker run --rm --name watchops-jaeger \
  -e COLLECTOR_OTLP_ENABLED=true \
  -p 16686:16686 \
  -p 4317:4317 \
  -p 4318:4318 \
  jaegertracing/all-in-one:latest
```

Enable WatchOps-Lite tracing in another terminal:

```bash
export WATCHOPS_TELEMETRY_ENABLED=true
export WATCHOPS_TELEMETRY_ENVIRONMENT=local
export WATCHOPS_TELEMETRY_OTLP_ENDPOINT=localhost:4317
export WATCHOPS_TELEMETRY_INSECURE=true
export WATCHOPS_TELEMETRY_SAMPLE_RATIO=1
make run
```

Send a Chat request, then open [http://localhost:16686](http://localhost:16686) and select the `watchops-lite` service. The response includes `X-Trace-ID`, and Chat responses also populate `trace_id`.

## Configuration

Configuration precedence is:

```text
defaults < JSON configuration file < environment variables
```

The default file is `configs/config.json`. Select another file with `-config` or `WATCHOPS_CONFIG_FILE`.

Common overrides:

```bash
export WATCHOPS_SERVER_ADDRESS=:9090
export WATCHOPS_LOG_LEVEL=debug
export WATCHOPS_TELEMETRY_ENABLED=false
make run
```

See [.env.example](.env.example) for the complete initial environment-variable set. The server does not automatically load `.env`; export the variables through your shell or development environment.

Telemetry uses OTLP over gRPC. `sample_ratio` controls a parent-based trace-ID ratio sampler; `1` is appropriate for local development. Span attributes contain identifiers, counts, status, and timing—not message bodies, retrieved content, credentials, or raw backend errors.

## Developer Commands

```bash
make run    # start the HTTP server
make test   # run unit tests
make lint   # run go vet
make fmt    # format Go source files
```

## Initial Layout

```text
.
├── cmd/server/                  # Process entry point
├── configs/                    # Example runtime configuration
├── docs/                       # Product and architecture documents
└── internal/
    ├── bootstrap/              # Application wiring and lifecycle
    ├── config/                 # Config loading and validation
    ├── observability/          # Structured logging and OTLP tracing
    ├── agent/eino/             # Eino ReAct, prompt/parser, tools, and fallback runner
    ├── application/chat/       # Chat use-case orchestration
    ├── memory/session/         # Redis session store and rolling summary
    ├── feedback/               # Feedback policy and MySQL store
    ├── eval/                   # Manual eval-seed policy and MySQL store
    ├── platform/elasticsearch/ # Official Elasticsearch client boundary
    ├── platform/mysql/         # database/sql client and schema boundary
    ├── retrieval/knowledge/    # Chunking, retrieval policy, and ES store
    ├── tools/                  # Contracts and deterministic mock tools
    └── transport/http/
        ├── handler/            # Thin Gin handlers
        └── middleware/         # Gin middleware
```

The complete planned layout is documented in [Project Blueprint](docs/PROJECT_BLUEPRINT.md). Directories for future features will be created only when their implementation begins.

## Design Principles

1. Evidence first: distinguish observed facts, inferences, and recommendations.
2. Clear boundaries: domain logic does not depend on transports or vendor SDKs.
3. Safe by default: future tools are read-only and their inputs are bounded.
4. Replaceable integrations: infrastructure connects through explicit ports.
5. Evaluation-ready: production feedback will become reviewed regression material.

## Design Documents

- [Project Blueprint](docs/PROJECT_BLUEPRINT.md)
- [HTTP API](docs/API.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Project Structure](docs/STRUCTURE.md)
- [Roadmap](docs/ROADMAP.md)
- [ADR 0001: Framework and Technology Stack](docs/adr/0001-framework-and-stack.md)
- [ADR 0002: Eino Tooling and WatchOps Tool Contracts](docs/adr/0002-eino-tooling.md)
- [ADR 0003: Deterministic Chat and Agent Skeleton](docs/adr/0003-chat-agent-skeleton.md)
- [ADR 0004: Redis Session Memory](docs/adr/0004-redis-session-memory.md)
- [ADR 0005: Elasticsearch Knowledge RAG](docs/adr/0005-elasticsearch-rag.md)
- [ADR 0006: MySQL Feedback and Eval Seed](docs/adr/0006-feedback-eval-seed.md)
- [ADR 0007: OpenTelemetry and Jaeger Tracing](docs/adr/0007-opentelemetry-jaeger-tracing.md)
- [ADR 0008: Eino ReAct Agent](docs/adr/0008-eino-react-agent.md)

## Originality

WatchOps-Lite is implemented from its own product requirements. It does not copy source code, project structure, prompts, comments, or documentation from Pilot or any training-camp project.

## License

Apache-2.0 is planned. A `LICENSE` file will be added before the first release.
