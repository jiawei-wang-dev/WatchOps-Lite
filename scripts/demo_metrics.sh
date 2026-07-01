#!/usr/bin/env bash
set -euo pipefail

# Requires curl and python3. Confirms that Prometheus scraped the demo exporter.
PROMETHEUS_URL="${WATCHOPS_PROMETHEUS_URL:-http://localhost:9090}"
METRIC_QUERY="${WATCHOPS_DEMO_METRIC_QUERY:-watchops_checkout_error_rate}"
response_file="$(mktemp)"
trap 'rm -f "${response_file}"' EXIT

if ! curl --fail --silent --show-error "${PROMETHEUS_URL}/-/ready" >/dev/null; then
  echo "Prometheus is unavailable at ${PROMETHEUS_URL}." >&2
  exit 1
fi

curl --fail-with-body --silent --show-error \
  --get "${PROMETHEUS_URL}/api/v1/query" \
  --data-urlencode "query=${METRIC_QUERY}" >"${response_file}"

python3 -c '
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    response = json.load(source)
results = response.get("data", {}).get("result", [])
if response.get("status") != "success" or not results:
    raise SystemExit("Prometheus returned no demo metric samples.")
sample = results[0]
print(json.dumps({
    "metric": sample.get("metric", {}).get("__name__", sys.argv[2]),
    "service": sample.get("metric", {}).get("service", ""),
    "value": sample.get("value", [None, None])[1],
    "status": "queryable"
}))
' "${response_file}" "${METRIC_QUERY}"
