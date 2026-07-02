#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
CASES_PATH="${WATCHOPS_AGENT_BENCHMARK_CASES:-${ROOT_DIR}/testdata/agent_benchmark_cases.json}"
OUTPUT_DIR="${WATCHOPS_AGENT_BENCHMARK_OUTPUT_DIR:-${ROOT_DIR}/tmp}"
REQUEST_TIMEOUT="${WATCHOPS_AGENT_BENCHMARK_TIMEOUT:-45s}"
CHECK_STREAM="${WATCHOPS_AGENT_BENCHMARK_STREAM:-true}"

if ! curl --fail --silent --show-error --max-time 5 "${API_BASE_URL}/healthz" >/dev/null; then
  cat >&2 <<EOF
WatchOps-Lite is not reachable at ${API_BASE_URL}.

Start the local dependencies and server first:
  docker compose up -d --wait
  make run CONFIG=configs/config.local.json

Seed the demo backends if you want real logs, metrics, traces, and knowledge evidence.
EOF
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"
cd "${ROOT_DIR}"
go run ./cmd/agent-benchmark \
  -base-url "${API_BASE_URL}" \
  -cases "${CASES_PATH}" \
  -timeout "${REQUEST_TIMEOUT}" \
  -stream="${CHECK_STREAM}" \
  -output-json "${OUTPUT_DIR}/agent_benchmark_report.json" \
  -output-markdown "${OUTPUT_DIR}/agent_benchmark_report.md"

printf "\nReports written to:\n"
printf "  %s\n" "${OUTPUT_DIR}/agent_benchmark_report.json"
printf "  %s\n" "${OUTPUT_DIR}/agent_benchmark_report.md"
