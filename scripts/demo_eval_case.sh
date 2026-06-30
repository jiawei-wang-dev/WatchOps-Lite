#!/usr/bin/env bash
set -euo pipefail

# Run demo_feedback.sh first so this bad case can retain its feedback provenance.
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
FEEDBACK_RESPONSE="${STATE_DIR}/feedback-response.json"

if [[ ! -f "${FEEDBACK_RESPONSE}" ]]; then
  echo "Missing ${FEEDBACK_RESPONSE}; run scripts/demo_feedback.sh first." >&2
  exit 1
fi

feedback_id="$(
  python3 -c '
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    response = json.load(source)
feedback_id = response.get("feedback_id")
if not feedback_id:
    raise SystemExit(
        "feedback_id is missing; feedback creation may have failed. "
        "Inspect " + sys.argv[1]
    )
print(feedback_id)
' "${FEEDBACK_RESPONSE}"
)"

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/eval/cases" \
  -H "Content-Type: application/json" \
  -d "{
    \"feedback_id\": \"${feedback_id}\",
    \"case_type\": \"bad_case\",
    \"input_message\": \"Why did the checkout error rate increase?\",
    \"expected_behavior\": \"Cite returned evidence and report missing real trace confirmation as a limitation.\",
    \"gold_answer\": \"\",
    \"forbidden_patterns\": [\"The payment service is definitely the root cause.\"],
    \"metadata\": {\"source\": \"mvp_demo\"}
  }"
printf "\n"

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/eval/cases?case_type=bad_case&limit=5"
printf "\n"
