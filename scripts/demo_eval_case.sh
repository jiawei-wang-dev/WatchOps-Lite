#!/usr/bin/env bash
set -euo pipefail

# Run demo_feedback.sh first so this bad case can retain its feedback provenance.
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
FEEDBACK_RESPONSE="${STATE_DIR}/feedback-response.json"
CASE_RESPONSE="${STATE_DIR}/eval-case-response.json"
DEMO_LANG="${WATCHOPS_DEMO_LANG:-en}"

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

case "${DEMO_LANG}" in
  en)
    input_message="Why did the checkout error rate increase?"
    expected_behavior="Cite returned evidence and report missing real trace confirmation as a limitation."
    forbidden_pattern="The payment service is definitely the root cause."
    ;;
  zh)
    input_message="checkout 服务错误率为什么升高？"
    expected_behavior="引用返回的证据，并将缺少真实 Trace 确认明确报告为局限性。"
    forbidden_pattern="payment 服务绝对是根因。"
    ;;
  *)
    echo "WATCHOPS_DEMO_LANG must be en or zh." >&2
    exit 2
    ;;
esac

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/eval/cases" \
  -H "Content-Type: application/json" \
  -d "{
    \"feedback_id\": \"${feedback_id}\",
    \"case_type\": \"bad_case\",
    \"input_message\": \"${input_message}\",
    \"expected_behavior\": \"${expected_behavior}\",
    \"gold_answer\": \"\",
    \"forbidden_patterns\": [\"${forbidden_pattern}\"],
    \"metadata\": {\"source\": \"mvp_demo\", \"language\": \"${DEMO_LANG}\"}
  }" | tee "${CASE_RESPONSE}"
printf "\n"

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/eval/cases?case_type=bad_case&limit=5"
printf "\n"
