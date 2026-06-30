# WatchOps-Lite

WatchOps-Lite is an Agentic RAG assistant for service reliability analysis. Its long-term goal is to combine operational knowledge with logs, metrics, and traces, then produce evidence-backed findings for on-call engineers and SRE teams.

The project is currently at the initial skeleton stage. It contains a runnable Go HTTP server, layered configuration, structured logging, and an OpenTelemetry integration boundary. RAG, Agent orchestration, persistence, and observability tools are intentionally not implemented yet.

## Current Scope

Implemented:

- Gin HTTP server with graceful shutdown
- `GET /healthz`
- JSON configuration with environment-variable overrides
- JSON structured logging with request metadata
- OpenTelemetry setup and shutdown placeholder
- Unit tests for configuration and health handling

Not implemented yet:

- Chat or Agent logic
- RAG ingestion and retrieval
- Redis or MySQL integration
- Logs, metrics, traces, or knowledge tools
- Feedback and evaluation workflows

## Requirements

- Go 1.23 or newer
- `make` for the convenience commands

No external services are required for the initial skeleton. Gin is the only direct third-party runtime dependency at this stage; its transitive modules are managed through `go.mod` and `go.sum`.

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
- [Architecture](docs/ARCHITECTURE.md)
- [Roadmap](docs/ROADMAP.md)

## Originality

WatchOps-Lite is implemented from its own product requirements. It does not copy source code, project structure, prompts, comments, or documentation from Pilot or any training-camp project.

## License

Apache-2.0 is planned. A `LICENSE` file will be added before the first release.
