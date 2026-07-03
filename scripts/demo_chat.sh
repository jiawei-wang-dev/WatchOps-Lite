#!/usr/bin/env bash
set -euo pipefail

# Saves the response so the feedback demo can reuse its request and session IDs.
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
DEMO_LANG="${WATCHOPS_DEMO_LANG:-en}"
mkdir -p "${STATE_DIR}"
payload="$(
  python3 -c '
import json
import sys
from datetime import datetime, timedelta, timezone

now = datetime.now(timezone.utc)
start = now - timedelta(minutes=20)
format_time = lambda value: value.isoformat(timespec="seconds").replace("+00:00", "Z")
language = sys.argv[1]
if language == "zh":
    message = (
        "checkout 服务错误率为什么升高？"
        "请结合指标、日志、告警和 runbook 给出有证据的诊断。"
    )
    session_id = "demo-checkout-session-zh"
elif language == "en":
    message = (
        "Why did the checkout error rate increase? "
        "Check metrics, logs, alerts, and the checkout runbook."
    )
    session_id = "demo-checkout-session"
else:
    raise SystemExit("WATCHOPS_DEMO_LANG must be en or zh")
print(json.dumps({
    "session_id": session_id,
    "message": message,
    "time_context": {
        "from": format_time(start),
        "to": format_time(now),
    },
}))
' "${DEMO_LANG}"
)"

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/chat" \
  -H "Content-Type: application/json" \
  -d "${payload}" | tee "${STATE_DIR}/chat-response.json"
printf "\n"
