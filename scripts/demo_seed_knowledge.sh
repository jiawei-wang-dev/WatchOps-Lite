#!/usr/bin/env bash
set -euo pipefail

# Requires curl and python3. No jq is needed.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_BASE_URL="${WATCHOPS_API_BASE_URL:-http://localhost:8080}"
DOCUMENT_PATH="${ROOT_DIR}/demo/knowledge/checkout_runbook.md"

payload="$(
  python3 -c '
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
print(json.dumps({
    "title": "Checkout Service High Error Rate Runbook",
    "source": "watchops-lite-demo",
    "content": path.read_text(encoding="utf-8"),
    "metadata": {
        "service": "checkout",
        "dependency": "payment",
        "category": "runbook"
    }
}))
' "${DOCUMENT_PATH}"
)"

curl --fail-with-body --silent --show-error \
  "${API_BASE_URL}/api/v1/knowledge/documents" \
  -H "Content-Type: application/json" \
  --data-binary "${payload}"
printf "\n"
