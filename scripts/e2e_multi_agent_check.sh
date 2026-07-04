#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
DEMO_LANG="${WATCHOPS_DEMO_LANG:-en}"
REQUEST_TIMEOUT="${WATCHOPS_MULTI_AGENT_TIMEOUT_SECONDS:-120}"

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e_multi_agent_check.sh [--lang en|zh]

Runs a lightweight check against an already-running WatchOps-Lite server:
the optional Multi-Agent JSON and SSE paths plus a Single-Agent compatibility
request. It does not start Docker Compose or require an external LLM key.
EOF
}

while (($# > 0)); do
  case "$1" in
    --lang)
      [[ -n "${2:-}" ]] || {
        printf 'FAIL  --lang requires en or zh\n' >&2
        exit 2
      }
      DEMO_LANG="$2"
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      printf 'FAIL  unknown option: %s\n' "$1" >&2
      exit 2
      ;;
  esac
  shift
done

if [[ "${DEMO_LANG}" != "en" && "${DEMO_LANG}" != "zh" ]]; then
  printf 'FAIL  unsupported demo language: %s\n' "${DEMO_LANG}" >&2
  exit 2
fi

STATE_DIR="${WATCHOPS_DEMO_STATE_DIR:-/tmp/watchops-lite-multi-agent-${DEMO_LANG}}"
mkdir -p "${STATE_DIR}"
cd "${ROOT_DIR}"

payload="$(
  python3 -c '
import json
import os
import sys
from datetime import datetime, timedelta, timezone

language = sys.argv[1]
now = datetime.now(timezone.utc)
start = now - timedelta(minutes=20)
fmt = lambda value: value.isoformat(timespec="seconds").replace("+00:00", "Z")
if language == "zh":
    message = "排查过去 20 分钟 checkout 错误率升高，并结合指标、日志、Trace 和 runbook 给出有证据边界的结论。"
    session_id = "multi-agent-demo-zh"
else:
    message = "Investigate the checkout error-rate increase over the last 20 minutes using metrics, logs, traces, and runbook evidence."
    session_id = "multi-agent-demo-en"
print(json.dumps({
    "session_id": session_id,
    "message": message,
    "time_context": {"from": fmt(start), "to": fmt(now)},
    "metadata": {"language": language, "demo": "multi_agent"},
}))
' "${DEMO_LANG}"
)"

printf '==> WatchOps-Lite health check\n'
curl --fail --silent --show-error --max-time 5 \
  "${API_BASE_URL}/healthz" >/dev/null

printf '==> Multi-Agent JSON response\n'
curl --fail-with-body --silent --show-error \
  --max-time "${REQUEST_TIMEOUT}" \
  "${API_BASE_URL}/api/v1/chat/multi-agent" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -d "${payload}" >"${STATE_DIR}/multi-agent-response.json"

python3 -c '
import json
import os
import sys

path, language = sys.argv[1:3]
with open(path, encoding="utf-8") as source:
    response = json.load(source)
if response.get("mode") != "multi_agent":
    raise SystemExit("Multi-Agent response mode is missing or invalid.")
roles = [step.get("role") for step in response.get("agent_steps", [])]
required = ["triage", "evidence", "knowledge", "synthesis"]
missing = [role for role in required if role not in roles]
if missing:
    raise SystemExit(f"Multi-Agent response is missing roles: {missing}")
answer = response.get("answer") or {}
for field in ("conclusion", "evidence", "recommendations", "limitations"):
    if field not in answer or not isinstance(answer[field], list):
        raise SystemExit(f"Multi-Agent answer field is invalid: {field}")
if not answer["evidence"]:
    raise SystemExit("Multi-Agent response contains no evidence.")
if not response.get("request_id") or not response.get("trace_id"):
    raise SystemExit("Multi-Agent response is missing request_id or trace_id.")
metadata = response.get("metadata") or {}
expect_setting = os.environ.get("WATCHOPS_EXPECT_MULTI_AGENT_LLM")
expect_llm = (
    expect_setting.lower() in ("1", "true", "yes")
    if expect_setting is not None
    else bool(os.environ.get("WATCHOPS_LLM_API_KEY"))
)
if expect_llm:
    if metadata.get("multi_agent_llm_call_count", 0) < 2:
        raise SystemExit("Multi-Agent response did not execute at least two LLM role calls.")
    if not (
        metadata.get("evidence_llm_used") or metadata.get("knowledge_llm_used")
    ):
        raise SystemExit("Evidence or Knowledge Agent did not use LLM analysis.")
    if not metadata.get("synthesis_llm_used"):
        raise SystemExit("Synthesis Agent did not use LLM analysis.")
    for role in ("evidence", "knowledge", "synthesis"):
        if metadata.get(f"{role}_llm_used") and not metadata.get(f"{role}_model"):
            raise SystemExit(f"{role} LLM metadata is missing its model.")
else:
    if metadata.get("multi_agent_llm_used"):
        raise SystemExit("Multi-Agent reported LLM use without an expected LLM key.")
    if metadata.get("multi_agent_llm_call_count") != 0:
        raise SystemExit("Multi-Agent reported LLM calls in deterministic fallback mode.")
    for role in ("evidence", "knowledge", "synthesis"):
        if metadata.get(f"{role}_fallback_used") is not True:
            raise SystemExit(f"{role} did not report deterministic fallback mode.")
if language == "zh":
    text = " ".join(
        str(item.get("text") or item.get("message") or "")
        for field in ("conclusion", "recommendations", "limitations")
        for item in answer[field]
    )
    if not any("\u4e00" <= char <= "\u9fff" for char in text):
        raise SystemExit("Chinese Multi-Agent response contains no Chinese text.")
print("Verified bounded Multi-Agent response and four role steps.")
' "${STATE_DIR}/multi-agent-response.json" "${DEMO_LANG}"

printf '==> Multi-Agent SSE response\n'
curl --no-buffer --fail-with-body --silent --show-error --http1.1 \
  --max-time "${REQUEST_TIMEOUT}" \
  "${API_BASE_URL}/api/v1/chat/multi-agent/stream" \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -d "${payload}" >"${STATE_DIR}/multi-agent-stream.sse"

python3 -c '
import json
import sys
import os

text = open(sys.argv[1], encoding="utf-8").read()
events = [
    line.removeprefix("event: ").strip()
    for line in text.splitlines()
    if line.startswith("event: ")
]
required = {
    "multi_agent_started",
    "agent_step_started",
    "agent_step_completed",
    "evidence_collected",
    "synthesis_started",
    "final_answer",
    "multi_agent_completed",
}
missing = sorted(required.difference(events))
if missing:
    raise SystemExit(f"Multi-Agent stream is missing events: {missing}")
if events.index("final_answer") > events.index("multi_agent_completed"):
    raise SystemExit("final_answer must precede multi_agent_completed.")
expect_setting = os.environ.get("WATCHOPS_EXPECT_MULTI_AGENT_LLM")
expect_llm = (
    expect_setting.lower() in ("1", "true", "yes")
    if expect_setting is not None
    else bool(os.environ.get("WATCHOPS_LLM_API_KEY"))
)
if expect_llm:
    for event in ("agent_llm_started", "agent_llm_completed"):
        if event not in events:
            raise SystemExit(f"Multi-Agent LLM stream is missing event: {event}")
print("Verified Multi-Agent SSE lifecycle and terminal event ordering.")
' "${STATE_DIR}/multi-agent-stream.sse"

printf '==> Single-Agent compatibility response\n'
curl --fail-with-body --silent --show-error \
  --max-time "${REQUEST_TIMEOUT}" \
  "${API_BASE_URL}/api/v1/chat" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -d "${payload}" >"${STATE_DIR}/single-agent-response.json"

python3 -c '
import json
import os
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    response = json.load(source)
if "answer" not in response or "tool_runs" not in response:
    raise SystemExit("Single-Agent compatibility response schema changed.")
if response.get("mode") == "multi_agent":
    raise SystemExit("Default Single-Agent endpoint returned Multi-Agent mode.")
print("Verified existing Single-Agent endpoint remains compatible.")
' "${STATE_DIR}/single-agent-response.json"

printf '\nMulti-Agent demo check passed (%s).\n' "${DEMO_LANG}"
printf 'Responses: %s\n' "${STATE_DIR}"
