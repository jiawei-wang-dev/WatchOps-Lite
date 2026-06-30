#!/usr/bin/env bash
set -euo pipefail

# Run demo_chat.sh first. The generated feedback ID is saved for demo_eval_case.sh.
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
CHAT_RESPONSE="${STATE_DIR}/chat-response.json"
FEEDBACK_RESPONSE="${STATE_DIR}/feedback-response.json"

if [[ ! -f "${CHAT_RESPONSE}" ]]; then
  echo "Missing ${CHAT_RESPONSE}; run scripts/demo_chat.sh first." >&2
  exit 1
fi

read -r request_id session_id < <(
  python3 -c '
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    response = json.load(source)
print(response["request_id"], response["session_id"])
' "${CHAT_RESPONSE}"
)

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/feedback" \
  -H "Content-Type: application/json" \
  -d "{
    \"request_id\": \"${request_id}\",
    \"session_id\": \"${session_id}\",
    \"rating\": \"down\",
    \"reason_tags\": [\"needs_trace_confirmation\"],
    \"comment\": \"The evidence is useful, but the payment timeout hypothesis still needs real trace confirmation.\",
    \"metadata\": {\"source\": \"mvp_demo\"}
  }" | tee "${FEEDBACK_RESPONSE}"
printf "\n"
