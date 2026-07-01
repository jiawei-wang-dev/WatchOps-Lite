#!/usr/bin/env bash
set -euo pipefail

# Saves the response so the feedback demo can reuse its request and session IDs.
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
mkdir -p "${STATE_DIR}"
payload="$(
  python3 -c '
import json
from datetime import datetime, timedelta, timezone

now = datetime.now(timezone.utc)
start = now - timedelta(minutes=20)
format_time = lambda value: value.isoformat(timespec="seconds").replace("+00:00", "Z")
print(json.dumps({
    "session_id": "demo-checkout-session",
    "message": "Why did the checkout error rate increase? Check metrics, logs, and the checkout runbook.",
    "time_context": {
        "from": format_time(start),
        "to": format_time(now),
    },
}))
'
)"

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/chat" \
  -H "Content-Type: application/json" \
  -d "${payload}" | tee "${STATE_DIR}/chat-response.json"
printf "\n"
