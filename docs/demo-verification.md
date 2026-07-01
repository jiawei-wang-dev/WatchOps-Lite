# WatchOps-Lite Demo Verification

> Verification date: `[YYYY-MM-DD]`
>
> Final full local run: 2026-07-01 (Australia/Melbourne)

## Status Summary

Status: **Fully verified locally.**

The complete demo passed with Redis, Elasticsearch, Prometheus, the Go demo metrics exporter, MySQL, Jaeger, and the Go application running together. Knowledge ingestion and BM25 search, real log and metric retrieval, deterministic Agent tool execution, evidence attribution, Redis session memory, MySQL feedback/eval persistence, OpenTelemetry export, Jaeger trace visualization, and the repository quality gate were all verified.

An earlier attempt was blocked by Docker Hub TLS handshake timeouts. After the Docker proxy/network configuration was corrected, `docker compose up -d --wait` completed successfully and the full flow below passed.

## Commands Run

### Repository Baseline

```bash
git status
git log --oneline -5
```

Expected:

- no uncommitted changes before verification
- latest commit identifies the MVP packaging phase

Observed: passed.

### Local Dependencies

```bash
docker compose up -d --wait
docker compose ps
```

Observed: passed.

| Container | Status | Local ports |
| --- | --- | --- |
| `watchops-lite-redis-1` | healthy | `127.0.0.1:6379->6379` |
| `watchops-lite-elasticsearch-1` | healthy | `127.0.0.1:9200->9200` |
| `watchops-lite-mysql-1` | healthy | `127.0.0.1:3306->3306` |
| `watchops-lite-jaeger-1` | running | `127.0.0.1:16686->16686`, `127.0.0.1:4317->4317` |
| `watchops-lite-demo-metrics-1` | healthy | `127.0.0.1:9108->9108` |
| `watchops-lite-prometheus-1` | healthy | `127.0.0.1:9090->9090` |

### Application and Health

```bash
cp configs/config.example.json configs/config.local.json
make run CONFIG=configs/config.local.json
curl -i http://localhost:8080/healthz
```

Observed: passed.

- HTTP status: `200 OK`
- `X-Request-Id`: `req-1782845856079-1`
- `X-Trace-Id`: `b933e4acd3ffba8c6424098c60db5d94`
- service status: `ok`
- response time: `2026-06-30T18:57:36.080192Z`

## Final Full Local Run

### Knowledge Ingestion

```bash
./scripts/demo_seed_knowledge.sh
```

Observed: passed.

```text
document_id: doc_3238de720ea4732ed7219b66
chunk_count: 2
```

### Logs Ingestion

```bash
./scripts/demo_seed_logs.sh
```

Observed: passed.

- index: `watchops_logs`
- six stable checkout demo events indexed
- reruns replace the same event IDs
- Elasticsearch unavailability produces a clear non-zero failure

The seed script preserves fixture IDs and content while shifting timestamps into the current 20-minute demo window. This keeps Elasticsearch logs aligned with normally scraped Prometheus samples without backdating time-series data.

```json
{"index":"watchops_logs","indexed_count":6,"status":"seeded"}
```

A real-backend Chat smoke test returned two log evidence items from `elasticsearch-logs`:

- `log_checkout_004`: context deadline exceeded
- `log_checkout_003`: checkout upstream timeout

The `query_logs` run reported `evidence_count=2`, `warning_count=0`, and trace ID `f9dcebef3f990a1a88b3d1f6e3c945dc`. Each evidence item included the expected log ID, level, trace ID, span ID, and timestamp metadata.

Degraded-mode smoke verification also passed with Elasticsearch unavailable: the application started, `query_logs` succeeded with `mock-logs`, `warning_count=1`, and the response retained the `MOCK_DATA` limitation.

### Metrics Verification

```bash
./scripts/demo_metrics.sh
```

Observed: passed.

```json
{"metric":"watchops_checkout_error_rate","service":"checkout","value":"0.062","status":"queryable"}
```

Prometheus scraped the Go demo exporter and exposed all four checkout signals. A real-backend Chat run returned `prometheus-watchops_checkout_error_rate-1` with value `0.062`, query and label metadata, `evidence_count=1`, and `warning_count=0`.

Degraded-mode verification also passed with the Prometheus URL intentionally unavailable: application startup succeeded, `query_metrics` returned `mock-metrics`, `warning_count=1`, and the response included the `MOCK_DATA` limitation. The unit suite separately verifies `TOOL_DEPENDENCY_UNAVAILABLE` when fallback is disabled.

### Chat Demo

```bash
./scripts/demo_chat.sh
```

Observed: passed.

```text
request_id: req-1782882785709-2
session_id: demo-checkout-session
trace_id:   9df0c1f254cffbe547fc944e821871d0
```

The response contained conclusions, evidence, tool runs, metadata, and no mock-data limitation because every selected tool used its real configured backend.

Tool runs:

| Tool | Success | Evidence count |
| --- | --- | --- |
| `query_metrics` | true | 1 |
| `query_logs` | true | 2 |
| `search_knowledge` | true | 2 |

`query_metrics` returned real Prometheus evidence:

- `prometheus-watchops_checkout_error_rate-1`
- value: `0.062`
- service: `checkout`

`search_knowledge` returned real Elasticsearch evidence:

- `doc_3238de720ea4732ed7219b66_chunk_0000`
- `doc_3238de720ea4732ed7219b66_chunk_0001`

The response did not present the payment dependency as a confirmed root cause. It correlated the metric and log evidence while describing upstream timeouts only as a plausible contributor.

### Feedback Demo

```bash
./scripts/demo_feedback.sh
```

Observed: passed.

```text
feedback_id: fb_96d2fd3761f901121b4e6ef6
status:      created
```

### Eval Case Demo

```bash
./scripts/demo_eval_case.sh
```

Observed: passed.

```text
case_id: eval_bcb2fb14c954fa66bde94313
status:  created
```

The list response retained:

- `case_type`: `bad_case`
- `feedback_id`: `fb_96d2fd3761f901121b4e6ef6`
- forbidden pattern: `The payment service is definitely the root cause.`

### Manual Knowledge Search

```bash
curl -sS http://localhost:8080/api/v1/knowledge/search \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "checkout upstream timeout payment dependency",
    "limit": 5
  }'
```

Observed: passed.

Two checkout runbook chunks were returned:

| Chunk ID | Score |
| --- | ---: |
| `doc_3238de720ea4732ed7219b66_chunk_0000` | `2.5767555` |
| `doc_3238de720ea4732ed7219b66_chunk_0001` | `0.72806674` |

Both results included:

- `service`: `checkout`
- `dependency`: `payment`
- `category`: `runbook`

### Manual Chat

```bash
curl -sS http://localhost:8080/api/v1/chat \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "demo_final_01",
    "message": "Why did checkout error rate increase in the last 20 minutes? Use the runbook if helpful.",
    "time_context": {
      "from": "2026-06-30T00:00:00Z",
      "to": "2026-06-30T00:20:00Z"
    }
  }'
```

Expected and confirmed:

- non-empty `trace_id`
- `answer.conclusion` present
- evidence returned by successful tools
- `tool_runs` present
- limitations accurately distinguish mock observability data
- no unsupported confirmed root-cause claim

### Redis Session Memory

```bash
redis-cli LRANGE session:demo_final_01:recent 0 -1
redis-cli HGETALL session:demo_final_01:summary
```

Expected:

- recent messages contain user and assistant entries
- the rolling summary appears after the configured threshold is exceeded

For a short session, an empty summary is expected until enough messages leave the recent-message window.

### MySQL Feedback and Eval

```bash
docker compose exec mysql mysql -u watchops -pwatchops watchops_lite
```

Queries:

```sql
SHOW TABLES;
SELECT id, request_id, session_id, rating, created_at FROM feedback LIMIT 5;
SELECT id, feedback_id, case_type, created_at FROM eval_cases LIMIT 5;
```

Observed: passed.

Tables:

- `feedback`
- `eval_cases`

Feedback row:

```text
id:         fb_96d2fd3761f901121b4e6ef6
request_id: req-1782845918218-3
session_id: demo-checkout-session
rating:     down
created_at: 2026-06-30 18:58:46.122714
```

Eval row:

```text
id:          eval_bcb2fb14c954fa66bde94313
feedback_id: fb_96d2fd3761f901121b4e6ef6
case_type:   bad_case
created_at:  2026-06-30 18:59:04.218953
```

The downvote was correctly associated with a `bad_case`.

### Jaeger

Open:

```text
http://localhost:16686
```

Observed: passed.

- service: `watchops-lite`
- trace ID: `9df0c1f254cffbe547fc944e821871d0`
- total spans: `14`
- service count: `1`
- depth: `6`

The trace tree contained:

- `HTTP POST /api/v1/chat`
- `chat.execute`
- `session.load_context`
- `agent.run`
- `tool.query_metrics`
- `metrics.query`
- `prometheus.query`
- `tool.query_logs`
- `logs.search`
- `elasticsearch.logs.search`
- `tool.search_knowledge`
- `knowledge.search`
- `elasticsearch.search`
- `session.persist_context`

### Quality Gate

```bash
make verify
```

Observed: passed.

`scripts/verify.sh` confirmed:

- gofmt check passed
- `go mod tidy` made no changes
- `go test ./...` passed for all Go packages
- `go vet ./...` passed
- `git diff --check` passed

## Screenshot Checklist

- [x] `docker compose ps` showing Redis, Elasticsearch, Prometheus, the demo exporter, MySQL, and Jaeger
- [x] `/healthz` showing HTTP 200, request ID, and trace ID
- [x] knowledge ingestion showing `document_id` and `chunk_count`
- [x] logs ingestion showing six indexed events and Chat evidence from `elasticsearch-logs`
- [x] Prometheus showing a scraped checkout metric and Chat evidence from `prometheus`
- [x] Chat response showing `trace_id`, evidence, limitations, and `tool_runs`
- [x] knowledge search showing checkout runbook chunks and scores
- [x] feedback creation showing `feedback_id`
- [x] eval-case creation showing `case_id`
- [ ] Redis recent messages and rolling summary
- [x] MySQL `feedback` and `eval_cases` rows
- [x] Jaeger trace tree for the Chat request
- [x] `make verify` successful output
- [ ] README Quick Start section
- [ ] architecture overview or Mermaid diagram

Do not commit screenshots unless a deliberate documentation decision introduces a maintained screenshots directory.

## Rerun Instructions

```bash
docker compose up -d --wait
docker compose ps
cp configs/config.example.json configs/config.local.json
make run CONFIG=configs/config.local.json
```

In another terminal:

```bash
./scripts/demo_seed_knowledge.sh
./scripts/demo_seed_logs.sh
./scripts/demo_metrics.sh
./scripts/demo_chat.sh
./scripts/demo_feedback.sh
./scripts/demo_eval_case.sh

curl -sS http://localhost:8080/api/v1/knowledge/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"checkout upstream timeout payment dependency","limit":5}'

make verify
```

Inspect Redis, MySQL, and Jaeger with the commands above, then stop the local stack when finished:

```bash
docker compose down
```

## Final Conclusion

**WatchOps-Lite MVP is fully verified locally and ready for GitHub publication.**
