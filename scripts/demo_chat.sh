#!/usr/bin/env bash
set -euo pipefail

# Saves the response so the feedback demo can reuse its request and session IDs.
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
mkdir -p "${STATE_DIR}"

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/chat" \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "demo-checkout-session",
    "message": "Why did the checkout error rate increase? Check metrics, logs, and the checkout runbook.",
    "time_context": {
      "from": "2026-06-30T00:00:00Z",
      "to": "2026-06-30T00:20:00Z"
    }
  }' | tee "${STATE_DIR}/chat-response.json"
printf "\n"
