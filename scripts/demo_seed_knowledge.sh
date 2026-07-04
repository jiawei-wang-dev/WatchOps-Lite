#!/usr/bin/env bash
set -euo pipefail

# Requires curl and python3. No jq is needed.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
DEMO_LANG="${WATCHOPS_DEMO_LANG:-en}"

if [[ "${1:-}" == "--lang" ]]; then
  [[ -n "${2:-}" ]] || {
    echo "--lang requires en or zh." >&2
    exit 2
  }
  DEMO_LANG="$2"
  shift 2
fi
if (($# > 0)); then
  echo "Usage: $0 [--lang en|zh]" >&2
  exit 2
fi

case "${DEMO_LANG}" in
  en)
    DOCUMENT_PATH="${ROOT_DIR}/demo/knowledge/checkout_runbook.md"
    DOCUMENT_TITLE="Checkout Service High Error Rate Runbook"
    ;;
  zh)
    DOCUMENT_PATH="${ROOT_DIR}/demo/knowledge/checkout_runbook_zh.md"
    DOCUMENT_TITLE="Checkout 服务高错误率排障 Runbook"
    ;;
  *)
    echo "Unsupported demo language: ${DEMO_LANG}; expected en or zh." >&2
    exit 2
    ;;
esac

payload="$(
  python3 -c '
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
print(json.dumps({
    "title": sys.argv[2],
    "source": "watchops-lite-demo",
    "content": path.read_text(encoding="utf-8"),
    "metadata": {
        "service": "checkout",
        "dependency": "payment",
        "category": "runbook",
        "language": sys.argv[3],
        "keywords": [
            "checkout", "payment", "timeout", "error_rate", "retry",
            "支付", "超时", "错误率", "重试"
        ]
    }
}))
' "${DOCUMENT_PATH}" "${DOCUMENT_TITLE}" "${DEMO_LANG}"
)"

response="$(
  curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/knowledge/documents" \
  -H "Content-Type: application/json" \
  --data-binary "${payload}"
)"

python3 -c '
import json
import sys

response = json.loads(sys.argv[1])
status = response.get("status", "seeded")
if status not in {"seeded", "skipped_duplicate", "already_exists"}:
    raise SystemExit(f"Unexpected knowledge seed status: {status}")
print(json.dumps(response, ensure_ascii=False))
print("Knowledge seed {}: document_id={} chunk_count={}".format(
    status,
    response.get("document_id", ""),
    response.get("chunk_count", 0),
))
' "${response}"
