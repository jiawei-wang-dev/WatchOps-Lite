#!/usr/bin/env bash
set -euo pipefail

# Run demo_eval_case.sh first so at least one bad_case exists.
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
mkdir -p "${STATE_DIR}"
RUN_RESPONSE="${STATE_DIR}/eval-run-response.json"

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/eval/runs" \
  -H "Content-Type: application/json" \
  -d '{"case_type":"bad_case","limit":20}' | tee "${RUN_RESPONSE}"
printf "\n"

run_id="$(
  python3 -c '
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    response = json.load(source)
run_id = response.get("run_id", "")
if not run_id:
    raise SystemExit("The eval run response did not contain run_id.")
print(run_id)
' "${RUN_RESPONSE}"
)"

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/eval/runs/${run_id}/results"
printf "\n"
