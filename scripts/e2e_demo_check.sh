#!/usr/bin/env bash
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-demo}"
SKIP_BENCHMARK=false
SKIP_EVAL=false
SKIP_SEED=false
GENERATE_LOGS=false
PASS_COUNT=0
WARN_COUNT=0

check_sse_file() {
  local stream_path="$1"
  local final_line completed_line
  [[ -f "${stream_path}" ]] || return 1
  final_line="$(
    grep -n -m 1 '^event: final_answer$' "${stream_path}" |
      cut -d: -f1
  )" || return 1
  completed_line="$(
    grep -n -m 1 '^event: workflow_completed$' "${stream_path}" |
      cut -d: -f1
  )" || return 1
  [[ -n "${final_line}" && -n "${completed_line}" ]] ||
    return 1
  ((final_line < completed_line))
}

if [[ -n "${WATCHOPS_E2E_CHECK_SSE_FILE:-}" ]]; then
  check_sse_file "${WATCHOPS_E2E_CHECK_SSE_FILE}"
  exit $?
fi

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e_demo_check.sh [options]

Options:
  --skip-benchmark  Skip the local Agent benchmark.
  --skip-eval       Skip retrieval and Agent eval.
  --skip-seed       Skip knowledge and log seeding.
  --generate-logs   Generate and seed a fresh checkout-timeout JSONL file.
  -h, --help        Show this help.

The command checks an already-running local stack. It never starts or stops
Docker Compose and does not require external API keys.
EOF
}

while (($# > 0)); do
  case "$1" in
    --skip-benchmark)
      SKIP_BENCHMARK=true
      ;;
    --skip-eval)
      SKIP_EVAL=true
      ;;
    --skip-seed)
      SKIP_SEED=true
      ;;
    --generate-logs)
      GENERATE_LOGS=true
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      printf 'FAIL  unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

mkdir -p "${STATE_DIR}"
cd "${ROOT_DIR}"

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  printf 'PASS  %s\n' "$1"
}

warn() {
  WARN_COUNT=$((WARN_COUNT + 1))
  printf 'WARN  %s\n' "$1"
}

finish_failed() {
  printf '\nEnd-to-end demo check failed after %d successful step(s) and %d warning(s).\n' \
    "${PASS_COUNT}" "${WARN_COUNT}" >&2
  exit 1
}

run_step() {
  local label="$1"
  shift
  printf '\n==> %s\n' "${label}"
  if "$@"; then
    pass "${label}"
  else
    printf 'FAIL  %s\n' "${label}" >&2
    finish_failed
  fi
}

stream_chat() {
  local payload stream_path
  stream_path="${STATE_DIR}/chat-stream.sse"
  payload="$(
    python3 -c '
import json
from datetime import datetime, timedelta, timezone

now = datetime.now(timezone.utc)
start = now - timedelta(minutes=20)
format_time = lambda value: value.isoformat(timespec="seconds").replace("+00:00", "Z")
print(json.dumps({
    "session_id": "demo-checkout-stream-session",
    "message": "Stream a checkout investigation using metrics, logs, and the runbook.",
    "time_context": {
        "from": format_time(start),
        "to": format_time(now),
    },
}))
'
  )" || return 1

  curl --no-buffer --fail-with-body --silent --show-error --http1.1 \
    "${API_BASE_URL}/api/v1/chat/stream" \
    -H "Accept: text/event-stream" \
    -H "Content-Type: application/json" \
    -d "${payload}" >"${stream_path}" || return 1
  check_sse_file "${stream_path}"
}

run_step "Dependency check" "${ROOT_DIR}/scripts/check_dependencies.sh"
run_step "WatchOps-Lite health check" \
  curl --fail --silent --show-error --max-time 5 "${API_BASE_URL}/healthz"

if [[ "${SKIP_SEED}" == "false" ]]; then
  run_step "Seed knowledge" "${ROOT_DIR}/scripts/demo_seed_knowledge.sh"
  if [[ "${GENERATE_LOGS}" == "true" ]]; then
    generated_logs="${STATE_DIR}/generated_checkout_logs.jsonl"
    run_step "Generate demo logs" \
      "${ROOT_DIR}/scripts/generate_demo_logs.sh" \
      --scenario checkout-timeout \
      --count 200 \
      --seed 42 \
      --output "${generated_logs}"
    run_step "Seed generated logs" \
      "${ROOT_DIR}/scripts/demo_seed_logs.sh" "${generated_logs}"
  else
    run_step "Seed fixture logs" "${ROOT_DIR}/scripts/demo_seed_logs.sh"
  fi
else
  warn "Knowledge and log seeding skipped"
fi

run_step "Verify Prometheus demo metrics" "${ROOT_DIR}/scripts/demo_metrics.sh"
run_step "Run normal Chat demo" "${ROOT_DIR}/scripts/demo_chat.sh"
run_step "Run SSE Chat demo" stream_chat

if [[ "${SKIP_EVAL}" == "false" ]]; then
  run_step "Run retrieval eval" \
    env WATCHOPS_RETRIEVAL_EVAL_OUTPUT="${STATE_DIR}/retrieval-eval-report.json" \
    "${ROOT_DIR}/scripts/eval_retrieval.sh"
  run_step "Create feedback seed" "${ROOT_DIR}/scripts/demo_feedback.sh"
  run_step "Create Agent eval case" "${ROOT_DIR}/scripts/demo_eval_case.sh"
  run_step "Run Agent eval" "${ROOT_DIR}/scripts/demo_eval_run.sh"
else
  warn "Retrieval and Agent eval skipped"
fi

if [[ "${SKIP_BENCHMARK}" == "false" ]]; then
  run_step "Run Agent benchmark" \
    env WATCHOPS_AGENT_BENCHMARK_OUTPUT_DIR="${STATE_DIR}" \
    "${ROOT_DIR}/scripts/benchmark_agent.sh"
else
  warn "Agent benchmark skipped"
fi

printf '\nEnd-to-end demo check passed: PASS=%d WARN=%d\n' \
  "${PASS_COUNT}" "${WARN_COUNT}"
printf 'Reports and responses: %s\n' "${STATE_DIR}"
printf 'Agent Console:  %s/\n' "${API_BASE_URL}"
printf 'Jaeger:        http://localhost:16686\n'
printf 'Grafana:       http://localhost:3000\n'
printf 'Prometheus:    http://localhost:9090\n'
printf '\nThis command validates the local demo path; it does not validate production scaling, paging, authentication, or remediation.\n'
