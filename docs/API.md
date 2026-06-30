# HTTP API

The current API is a deterministic skeleton. It validates the transport and evidence flow before real LLM, memory, retrieval, and observability integrations are introduced.

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
        "text": "Mock metrics report elevated latency and error rate.",
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
        "message": "This response uses deterministic mock evidence and has not queried production systems."
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
  "trace_id": ""
}
```

The actual error-rate route calls both `query_metrics` and `query_logs`; the shortened example above shows one result.

### Deterministic Routing

The Phase 3 skeleton uses transparent rules:

| Message contains | Mock tool calls |
| --- | --- |
| `error rate` | `query_metrics`, `query_logs` |
| `trace` or `slow` | `query_traces` |
| `runbook`, `playbook`, `knowledge`, `how should`, or `how do` | `search_knowledge` |
| No recognized phrase | No tool call; `MORE_CONTEXT_REQUIRED` limitation |

This is not a production ReAct loop and does not call an LLM.

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
