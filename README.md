# WatchOps-Lite

WatchOps-Lite is an Agentic RAG assistant for service reliability analysis. Its long-term goal is to combine operational knowledge with logs, metrics, and traces, then produce evidence-backed findings for on-call engineers and SRE teams.

The project currently contains the Gin HTTP skeleton, Eino-backed mock tools, and a deterministic Chat/Agent skeleton. It validates request, tool, evidence, and answer contracts without calling a real LLM or backend.

## Current Scope

Implemented:

- Gin HTTP server with graceful shutdown
- `GET /healthz`
- JSON configuration with environment-variable overrides
- JSON structured logging with request metadata
- OpenTelemetry setup and shutdown placeholder
- Unit tests for configuration and health handling
- Shared `ToolResult`, `ToolError`, and evidence contracts
- Timeout and fallback execution wrapper
- Eino-backed mock tools for logs, metrics, traces, and knowledge
- `POST /api/v1/chat` with deterministic tool routing
- Evidence-aware answers and tool run summaries

Not implemented yet:

- Real `ChatModel`, Eino Graph, or production ReAct Agent logic
- RAG ingestion and retrieval
- Redis or MySQL integration
- Real logs, metrics, traces, or knowledge backends
- Feedback and evaluation workflows

## Requirements

- Go 1.23 or newer
- `make` for the convenience commands

No external services are required for the current skeleton. Gin and Eino are the direct third-party runtime dependencies at this stage; their transitive modules are managed through `go.mod` and `go.sum`.

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

Call the deterministic Chat endpoint:

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

This route uses deterministic mock evidence. See [HTTP API](docs/API.md) for the request, response, routing, and error contracts.

Example response:

```json
{
  "status": "ok",
  "service": "watchops-lite",
  "time": "2026-06-30T00:00:00Z"
}
```

Stop the server with `Ctrl+C`. It handles `SIGINT` and `SIGTERM` with a bounded graceful shutdown.

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

OpenTelemetry is currently a lifecycle placeholder. Enabling it records a structured warning but does not export telemetry until the SDK and OTLP exporter are introduced in a later task.

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
    ├── observability/          # Structured logging and OTel boundary
    ├── agent/eino/             # Eino tools and deterministic Agent runner
    ├── application/chat/       # Chat use-case orchestration
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

## Originality

WatchOps-Lite is implemented from its own product requirements. It does not copy source code, project structure, prompts, comments, or documentation from Pilot or any training-camp project.

## License

Apache-2.0 is planned. A `LICENSE` file will be added before the first release.
