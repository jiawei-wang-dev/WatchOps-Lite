#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
CASES_PATH="${WATCHOPS_RETRIEVAL_EVAL_CASES:-${ROOT_DIR}/testdata/retrieval_eval_cases.json}"
TOP_K="${WATCHOPS_RETRIEVAL_EVAL_TOP_K:-5}"
OUTPUT_PATH="${WATCHOPS_RETRIEVAL_EVAL_OUTPUT:-}"

if ! curl --fail --silent --show-error --max-time 5 "${API_BASE_URL}/healthz" >/dev/null; then
  cat >&2 <<EOF
WatchOps-Lite is not reachable at ${API_BASE_URL}.

Start local dependencies and the server first:
  docker compose up -d --wait
  make run CONFIG=configs/config.local.json

Then seed knowledge:
  ./scripts/demo_seed_knowledge.sh
EOF
  exit 1
fi

args=(
  "run" "./cmd/retrieval-eval"
  "-cases" "${CASES_PATH}"
  "-base-url" "${API_BASE_URL}"
  "-top-k" "${TOP_K}"
)
if [[ -n "${OUTPUT_PATH}" ]]; then
  args+=("-output" "${OUTPUT_PATH}")
fi

cd "${ROOT_DIR}"
go "${args[@]}"
