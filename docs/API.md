# HTTP API

The current API combines configurable Eino ReAct or deterministic Chat execution, Redis session memory, Elasticsearch knowledge retrieval, MySQL-backed feedback/eval seeds, and OpenTelemetry tracing.

## Local MVP Demo

Start the Compose dependencies and application, then run the complete API sequence:

```bash
docker compose up -d --wait
cp configs/config.example.json configs/config.local.json
make run CONFIG=configs/config.local.json
```

In another terminal:

```bash
./scripts/demo_seed_knowledge.sh
./scripts/demo_chat.sh
./scripts/demo_feedback.sh
./scripts/demo_eval_case.sh
```

The scripts use `http://localhost:8080` by default, require `curl` and Python 3, and do not require jq. Chat and feedback response IDs are retained under `/tmp/watchops-lite-demo` so the next API call can preserve provenance.

## Trace Correlation

When telemetry is enabled and a span context is valid, every HTTP response includes:

```http
X-Trace-ID: 4bf92f3577b34da6a3ce929d0e0e4736
```

Incoming W3C `traceparent` and `baggage` headers are extracted so WatchOps-Lite can join an existing distributed trace. Chat responses also populate the existing top-level `trace_id` and `metadata.trace_id`. When telemetry is disabled, no trace header is added and the Chat `trace_id` remains empty.

## Health Check

```http
GET /healthz
```

Successful response:

```json
{
  "status": "ok",
  "service": "watchops-lite",
  "time": "2026-06-30T00:00:00Z"
}
```

## Chat

```http
POST /api/v1/chat
Content-Type: application/json
```

Request:

```json
{
  "session_id": "ses_01",
  "message": "Why did checkout error rate increase in the last 20 minutes?",
  "time_context": {
    "from": "2026-06-30T00:00:00Z",
    "to": "2026-06-30T00:20:00Z"
  }
}
```

`time_context.from` and `time_context.to` must be RFC3339 timestamps, and `to` must not be earlier than `from`.

Successful response shape:

```json
{
  "request_id": "req_01",
  "session_id": "ses_01",
  "answer": {
    "conclusion": [
      {
        "text": "Metric evidence reports the requested service reliability signal.",
        "evidence_ids": ["metric-evidence-001"]
      }
    ],
    "evidence": [
      {
        "id": "metric-evidence-001",
        "source_type": "metrics",
        "source_name": "mock-metrics",
        "time_range": {
          "from": "2026-06-30T00:00:00Z",
          "to": "2026-06-30T00:20:00Z"
        },
        "content": "Mock metrics show p95 latency at 1.8s and an error rate of 6.2% for service checkout.",
        "resource_id": "checkout",
        "confidence": 0.95,
        "metadata": {}
      }
    ],
    "inferences": [],
    "recommendations": [
      {
        "text": "Compare the affected interval with the service baseline and dependency metrics.",
        "evidence_ids": ["metric-evidence-001"]
      }
    ],
    "limitations": [
      {
        "code": "MOCK_DATA",
        "message": "This response includes deterministic mock evidence for one or more tools and is not a production-only investigation."
      }
    ]
  },
  "tool_runs": [
    {
      "tool": "query_metrics",
      "success": true,
      "duration_ms": 0,
      "evidence_count": 1,
      "warning_count": 0
    }
  ],
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "metadata": {
    "agent_mode": "deterministic",
    "fallback_used": false,
    "session_context_loaded": false,
    "recent_message_count": 0,
    "summary_version": 0,
    "session_memory_available": true,
    "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
  }
}
```

The actual error-rate route calls both `query_metrics` and `query_logs`; the shortened example above shows one result.

### Agent Modes

`agent.mode=deterministic` remains the default and uses transparent rules:

| Message contains | Mock tool calls |
| --- | --- |
| `error rate` | `query_metrics`, `query_logs` |
| `trace` or `slow` | `query_traces` |
| `runbook`, `playbook`, `knowledge`, `how should`, or `how do` | `search_knowledge` |
| No recognized phrase | No tool call; `MORE_CONTEXT_REQUIRED` limitation |

With `agent.mode=eino_react` and complete LLM configuration, Eino's ReAct Graph exposes the same four tools to the configured OpenAI-compatible ChatModel. The model may select tools iteratively within the configured timeout and maximum-iteration bound.

Eino responses include metadata such as:

```json
{
  "agent_mode": "eino_react",
  "prompt_version": "watchops_agent_v1",
  "model": "configured-model",
  "max_iterations": 6,
  "tool_names": ["query_metrics"],
  "output_parse_success": true,
  "fallback_used": false
}
```

Only evidence returned by tools is copied into `answer.evidence`. Conclusions and inferences referencing invented or missing evidence IDs are removed and reported through `EVIDENCE_REFERENCE_INVALID`.

If a model call fails, the request uses the deterministic fallback and adds:

```json
{
  "code": "AGENT_LLM_FALLBACK",
  "message": "The LLM Agent was unavailable; the deterministic runner handled this request."
}
```

Malformed final JSON produces `AGENT_OUTPUT_PARSE_FAILED`; raw model output and model errors are not exposed.

### Session Context

Before Agent execution, the Chat service loads the Redis rolling summary and recent-message window. The response metadata reports:

- `session_context_loaded`: whether prior summary or recent messages were present
- `recent_message_count`: number of raw messages passed to the Agent
- `summary_version`: optimistic revision loaded for this request
- `session_memory_available`: whether Redis memory operations succeeded

If Redis is unavailable, Chat still returns a normal answer for the current turn. `session_memory_available` is `false`, and `answer.limitations` contains:

```json
{
  "code": "SESSION_MEMORY_UNAVAILABLE",
  "message": "Session memory is unavailable; this response was generated without durable conversation context."
}
```

Raw Redis errors are not returned.

### Tool Failures

Tool failures do not disappear from the answer. A failed tool has `success: false` and an `error_code` in `tool_runs`, plus a corresponding entry in `answer.limitations`. No evidence is created for a failed call.

### Invalid Request

Invalid HTTP input returns:

```json
{
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "request body is invalid",
    "request_id": "req_01",
    "details": []
  }
}
```

The response status is `400 Bad Request`. Unexpected internal errors use a safe `INTERNAL` message without exposing raw errors.

## Knowledge Documents

Elasticsearch must be enabled for the knowledge endpoints. Plain text and Markdown are accepted as a `content` string.

```http
POST /api/v1/knowledge/documents
Content-Type: application/json
```

Request:

```json
{
  "title": "Checkout runbook",
  "source": "manual",
  "content": "Check upstream timeout saturation.\n\nCompare retry volume with latency.",
  "metadata": {
    "service": "checkout",
    "category": "runbook"
  }
}
```

Successful response (`201 Created`):

```json
{
  "document_id": "doc_0123456789abcdef01234567",
  "chunk_count": 2
}
```

The service splits content by paragraphs, merges text up to the configured chunk size, and assigns stable chunk IDs from the document ID and chunk index.

## Knowledge Search

```http
POST /api/v1/knowledge/search
Content-Type: application/json
```

Request:

```json
{
  "query": "checkout timeout",
  "limit": 5,
  "filters": {
    "service": "checkout"
  }
}
```

Successful response:

```json
{
  "results": [
    {
      "chunk_id": "doc_0123456789abcdef01234567_chunk_0000",
      "document_id": "doc_0123456789abcdef01234567",
      "title": "Checkout runbook",
      "content": "Check upstream timeout saturation.",
      "source": "manual",
      "score": 0.0325,
      "retrieval_mode": "hybrid",
      "bm25_score": 3.42,
      "vector_score": 0.91,
      "rrf_score": 0.0325,
      "metadata": {
        "service": "checkout",
        "category": "runbook"
      }
    }
  ]
}
```

`limit` defaults to the configured final result count and must be between 1 and 20. Retrieval mode is configured as `bm25`, `vector`, or `hybrid`. Hybrid mode uses RRF and can explicitly fall back to BM25 when embeddings are disabled or unavailable. Score component fields are omitted when they do not apply.

## Knowledge Document Metadata

```http
GET /api/v1/knowledge/documents/{document_id}
```

This phase does not maintain a separate durable document record. The endpoint reconstructs title, source, metadata, creation time, and chunk count from indexed chunks; it does not return the original full document.

When Elasticsearch is disabled or unavailable, knowledge endpoints return `503 Service Unavailable` with the safe error code `DEPENDENCY_UNAVAILABLE`.

## Feedback

```http
POST /api/v1/feedback
Content-Type: application/json
```

Request:

```json
{
  "request_id": "req-123",
  "session_id": "ses_01",
  "rating": "down",
  "reason_tags": ["missing_evidence", "too_vague"],
  "comment": "The answer did not cite trace evidence.",
  "corrected_answer": "The likely issue is upstream timeout, but trace evidence is still missing.",
  "answer_snapshot": {
    "conclusion": ["The error rate increased."],
    "limitations": ["Trace evidence is unavailable."]
  },
  "evidence_ids": ["metric-evidence-001", "log-evidence-001"],
  "tool_runs": [
    {
      "tool": "query_metrics",
      "success": true,
      "duration_ms": 35
    }
  ],
  "metadata": {
    "prompt_version": "answer_schema_v1"
  }
}
```

`rating` must be `up` or `down`. Successful response (`201 Created`):

```json
{
  "feedback_id": "fb_0123456789abcdef01234567",
  "status": "created"
}
```

Fetch the stored record:

```http
GET /api/v1/feedback/{feedback_id}
```

The response contains the normalized request fields plus `id` and `created_at`.

## Eval Case Seeds

Eval cases are created manually. Providing `feedback_id` verifies that the source feedback exists and enforces:

- an upvote can seed a `good_case`
- a downvote can seed a `bad_case`

```http
POST /api/v1/eval/cases
Content-Type: application/json
```

Request:

```json
{
  "feedback_id": "fb_0123456789abcdef01234567",
  "case_type": "bad_case",
  "input_message": "Why did checkout error rate increase?",
  "expected_behavior": "The answer must cite evidence and mention missing trace data as a limitation.",
  "gold_answer": "",
  "forbidden_patterns": [
    "Do not claim root cause without evidence."
  ],
  "metadata": {
    "source": "manual_feedback"
  }
}
```

Successful response (`201 Created`):

```json
{
  "case_id": "eval_0123456789abcdef01234567",
  "status": "created"
}
```

List cases:

```http
GET /api/v1/eval/cases?case_type=bad_case&limit=20
```

`case_type` is optional. `limit` defaults to 50 and must be between 1 and 100.

Missing source feedback returns `404`. Validation failures return `400`. When MySQL is disabled or unavailable, feedback and eval endpoints return `503 DEPENDENCY_UNAVAILABLE`. Database errors are not exposed.
