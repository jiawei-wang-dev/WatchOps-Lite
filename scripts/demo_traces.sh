#!/usr/bin/env bash
set -euo pipefail

# Creates a fresh Chat trace, waits for Jaeger export, then asks Chat to
# correlate metrics, logs, knowledge, and the exact Jaeger trace.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
JAEGER_URL="${WATCHOPS_JAEGER_URL:-http://localhost:16686}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
mkdir -p "${STATE_DIR}"

"${ROOT_DIR}/scripts/demo_chat.sh" >/dev/null
trace_id="$(
  python3 -c '
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    response = json.load(source)
trace_id = response.get("trace_id", "")
if not trace_id:
    raise SystemExit("The demo Chat response did not contain a trace_id.")
print(trace_id)
' "${STATE_DIR}/chat-response.json"
)"

trace_ready=false
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if curl --fail --silent --show-error --max-time 2 \
    "${JAEGER_URL}/api/traces/${trace_id}" >/dev/null 2>&1; then
    trace_ready=true
    break
  fi
  sleep 1
done
if [[ "${trace_ready}" != "true" ]]; then
  echo "Trace ${trace_id} was not queryable from Jaeger at ${JAEGER_URL}." >&2
  exit 1
fi

payload="$(
  python3 -c '
import json
import sys
from datetime import datetime, timedelta, timezone

now = datetime.now(timezone.utc)
start = now - timedelta(minutes=20)
format_time = lambda value: value.isoformat(timespec="seconds").replace("+00:00", "Z")
print(json.dumps({
    "session_id": "demo-checkout-trace-session",
    "message": (
        "Why did the checkout error rate increase? Check metrics, logs, the checkout "
        "runbook, and trace " + sys.argv[1] + "; explain whether any span looks slow."
    ),
    "time_context": {
        "from": format_time(start),
        "to": format_time(now),
    },
}))
' "${trace_id}"
)"

response_path="${STATE_DIR}/trace-chat-response.json"
curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/chat" \
  -H "Content-Type: application/json" \
  -d "${payload}" >"${response_path}"

python3 -c '
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    response = json.load(source)
evidence = response.get("answer", {}).get("evidence", [])
sources = sorted({item.get("source_name", "") for item in evidence if item.get("source_name")})
trace_evidence = [item for item in evidence if item.get("source_name") == "jaeger"]
if not trace_evidence:
    raise SystemExit("Chat returned no Jaeger trace evidence.")
print(json.dumps({
    "queried_trace_id": sys.argv[2],
    "chat_trace_id": response.get("trace_id", ""),
    "source_names": sources,
    "jaeger_evidence_count": len(trace_evidence),
    "status": "verified",
}))
' "${response_path}" "${trace_id}"
