#!/usr/bin/env bash
set -euo pipefail

# Run demo_eval_case.sh first. This script intentionally evaluates only the
# case created by that invocation, so historical demo cases do not add LLM
# calls to the end-to-end check.
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
REQUEST_TIMEOUT="${WATCHOPS_AGENT_EVAL_TIMEOUT_SECONDS:-120}"
mkdir -p "${STATE_DIR}"
CASE_RESPONSE="${STATE_DIR}/eval-case-response.json"
RUN_RESPONSE="${STATE_DIR}/eval-run-response.json"
RESULTS_RESPONSE="${STATE_DIR}/eval-run-results.json"

if [[ ! "${REQUEST_TIMEOUT}" =~ ^[1-9][0-9]*$ ]]; then
  echo "WATCHOPS_AGENT_EVAL_TIMEOUT_SECONDS must be a positive integer." >&2
  exit 2
fi
if [[ ! -f "${CASE_RESPONSE}" ]]; then
  echo "Missing ${CASE_RESPONSE}; run scripts/demo_eval_case.sh first." >&2
  exit 1
fi

case_id="$(
  python3 -c '
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    response = json.load(source)
case_id = response.get("case_id", "")
if not case_id:
    raise SystemExit(
        "case_id is missing; eval case creation may have failed. "
        "Inspect " + sys.argv[1]
    )
print(case_id)
' "${CASE_RESPONSE}"
)"

if curl --fail-with-body --silent --show-error \
  --connect-timeout 5 \
  --max-time "${REQUEST_TIMEOUT}" \
  "${API_BASE_URL}/api/v1/eval/runs" \
  -H "Content-Type: application/json" \
  -d '{"case_type":"bad_case","limit":1}' \
  -o "${RUN_RESPONSE}"; then
  :
else
  curl_status=$?
  echo "Agent eval request failed (curl exit ${curl_status})." >&2
  echo "The E2E run was limited to current case ${case_id} and allowed ${REQUEST_TIMEOUT}s." >&2
  echo "Inspect the WatchOps-Lite server logs and ${RUN_RESPONSE} if it exists." >&2
  exit "${curl_status}"
fi
cat "${RUN_RESPONSE}"
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

if curl --fail-with-body --silent --show-error \
  --connect-timeout 5 \
  --max-time 15 \
  "${API_BASE_URL}/api/v1/eval/runs/${run_id}/results" \
  -o "${RESULTS_RESPONSE}"; then
  :
else
  curl_status=$?
  echo "Fetching Agent eval results failed (curl exit ${curl_status}) for run ${run_id}." >&2
  exit "${curl_status}"
fi
cat "${RESULTS_RESPONSE}"
printf "\n"

python3 -c '
import json
import sys

expected_case_id = sys.argv[1]
run_path = sys.argv[2]
results_path = sys.argv[3]
with open(run_path, encoding="utf-8") as source:
    run = json.load(source)
with open(results_path, encoding="utf-8") as source:
    results = json.load(source).get("results", [])
if run.get("total") != 1:
    raise SystemExit(
        f"Expected the E2E Agent eval to run one case, got {run.get('total')!r}."
    )
case_ids = [result.get("case_id") for result in results]
if case_ids != [expected_case_id]:
    raise SystemExit(
        "The E2E Agent eval did not execute the case created by this run: "
        f"expected {[expected_case_id]!r}, got {case_ids!r}."
    )
print(f"Verified Agent eval scope: current case {expected_case_id} only.")
' "${case_id}" "${RUN_RESPONSE}" "${RESULTS_RESPONSE}"
