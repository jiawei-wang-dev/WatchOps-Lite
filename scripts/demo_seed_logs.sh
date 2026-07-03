#!/usr/bin/env bash
set -euo pipefail

# Requires curl and python3. Uses stable document IDs, so reruns replace demo events.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ELASTICSEARCH_URL="${WATCHOPS_ELASTICSEARCH_URL:-http://localhost:9200}"
LOGS_INDEX="${WATCHOPS_LOGS_INDEX:-watchops_logs}"
DEMO_LANG="${WATCHOPS_DEMO_LANG:-en}"
LOGS_PATH=""

while (($# > 0)); do
  case "$1" in
    --lang)
      [[ -n "${2:-}" ]] || {
        echo "--lang requires en or zh." >&2
        exit 2
      }
      DEMO_LANG="$2"
      shift 2
      ;;
    -*)
      echo "Unknown option: $1" >&2
      exit 2
      ;;
    *)
      [[ -z "${LOGS_PATH}" ]] || {
        echo "Only one custom JSONL path can be provided." >&2
        exit 2
      }
      LOGS_PATH="$1"
      shift
      ;;
  esac
done

if [[ -z "${LOGS_PATH}" ]]; then
  case "${DEMO_LANG}" in
    en) LOGS_PATH="${ROOT_DIR}/demo/logs/checkout_logs.jsonl" ;;
    zh) LOGS_PATH="${ROOT_DIR}/demo/logs/checkout_logs_zh.jsonl" ;;
    *)
      echo "Unsupported demo language: ${DEMO_LANG}; expected en or zh." >&2
      exit 2
      ;;
  esac
fi
bulk_file="$(mktemp)"
response_file="$(mktemp)"
trap 'rm -f "${bulk_file}" "${response_file}"' EXIT

if [[ ! -f "${LOGS_PATH}" ]]; then
  echo "Log JSONL file does not exist: ${LOGS_PATH}" >&2
  exit 1
fi

if ! curl --fail --silent --show-error "${ELASTICSEARCH_URL}" >/dev/null; then
  echo "Elasticsearch is unavailable at ${ELASTICSEARCH_URL}." >&2
  exit 1
fi

index_status="$(
  curl --silent --output /dev/null --write-out "%{http_code}" \
    --head "${ELASTICSEARCH_URL}/${LOGS_INDEX}"
)"
if [[ "${index_status}" == "404" ]]; then
  curl --fail-with-body --silent --show-error \
    -X PUT "${ELASTICSEARCH_URL}/${LOGS_INDEX}" \
    -H "Content-Type: application/json" \
    -d '{
      "mappings": {
        "properties": {
          "id": {"type": "keyword"},
          "timestamp": {"type": "date"},
          "service": {"type": "keyword"},
          "level": {"type": "keyword"},
          "message": {"type": "text"},
          "trace_id": {"type": "keyword"},
          "span_id": {"type": "keyword"},
          "attributes": {"type": "object", "dynamic": true},
          "created_at": {"type": "date"}
        }
      }
    }' >/dev/null
elif [[ "${index_status}" != "200" ]]; then
  echo "Unable to inspect Elasticsearch index ${LOGS_INDEX}: HTTP ${index_status}." >&2
  exit 1
fi

python3 -c '
import json
import pathlib
import sys
from datetime import datetime, timedelta, timezone

source = pathlib.Path(sys.argv[1])
index = sys.argv[2]
with source.open(encoding="utf-8") as events:
    records = [json.loads(line) for line in events if line.strip()]
fixture_start = min(datetime.fromisoformat(item["timestamp"].replace("Z", "+00:00")) for item in records)
window_start = datetime.now(timezone.utc) - timedelta(minutes=20)
for event in records:
    fixture_time = datetime.fromisoformat(event["timestamp"].replace("Z", "+00:00"))
    shifted_time = window_start + (fixture_time - fixture_start)
    encoded_time = shifted_time.isoformat(timespec="seconds").replace("+00:00", "Z")
    event["timestamp"] = encoded_time
    event["created_at"] = encoded_time
    print(json.dumps({"index": {"_index": index, "_id": event["id"]}}))
    print(json.dumps(event, separators=(",", ":")))
' "${LOGS_PATH}" "${LOGS_INDEX}" >"${bulk_file}"

curl --fail-with-body --silent --show-error \
  -X POST "${ELASTICSEARCH_URL}/_bulk?refresh=wait_for" \
  -H "Content-Type: application/x-ndjson" \
  --data-binary "@${bulk_file}" >"${response_file}"

python3 -c '
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    response = json.load(source)
if response.get("errors"):
    raise SystemExit("Elasticsearch rejected one or more demo log events.")
print(json.dumps({
    "index": sys.argv[2],
    "indexed_count": len(response.get("items", [])),
    "status": "seeded"
}))
' "${response_file}" "${LOGS_INDEX}"
