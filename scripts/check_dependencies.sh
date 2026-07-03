#!/usr/bin/env bash
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

PASS_COUNT=0
WARN_COUNT=0
FAIL_COUNT=0

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  printf 'PASS  %s\n' "$1"
}

warn() {
  WARN_COUNT=$((WARN_COUNT + 1))
  printf 'WARN  %s\n' "$1"
}

fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  printf 'FAIL  %s\n' "$1"
}

check_command() {
  local command_name="$1"
  if command -v "${command_name}" >/dev/null 2>&1; then
    pass "${command_name} is available"
  else
    fail "${command_name} is required but was not found"
  fi
}

check_file() {
  local file_path="$1"
  if [[ -f "${file_path}" ]]; then
    pass "${file_path} exists"
  else
    fail "${file_path} is required"
  fi
}

check_port() {
  local name="$1"
  local port="$2"
  if command -v nc >/dev/null 2>&1 &&
    nc -z -w 1 127.0.0.1 "${port}" >/dev/null 2>&1; then
    pass "${name} port ${port} is reachable"
  else
    warn "${name} port ${port} is not listening"
  fi
}

check_http() {
  local name="$1"
  local url="$2"
  if command -v curl >/dev/null 2>&1 &&
    curl --fail --silent --show-error --max-time 2 "${url}" >/dev/null 2>&1; then
    pass "${name} is reachable at ${url}"
  else
    warn "${name} is not reachable at ${url}"
  fi
}

printf 'WatchOps-Lite dependency check\n\n'

check_command go
if command -v go >/dev/null 2>&1; then
  go_version="$(go env GOVERSION 2>/dev/null || true)"
  case "${go_version}" in
    go1.2[3-9]* | go1.[3-9][0-9]*)
      pass "Go version ${go_version} satisfies Go 1.23+"
      ;;
    *)
      fail "Go 1.23+ is required; found ${go_version:-unknown}"
      ;;
  esac
fi

check_command docker
if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    pass "Docker Compose is available"
  else
    fail "Docker Compose v2 is required"
  fi
  if docker info >/dev/null 2>&1; then
    pass "Docker daemon is running"
  else
    warn "Docker daemon is not running"
  fi
fi

check_command curl
check_command python3

config_path="${CONFIG:-configs/config.local.json}"
if [[ -f "${config_path}" ]]; then
  pass "configuration exists at ${config_path}"
else
  warn "${config_path} is missing; copy configs/config.example.json before running the local app"
fi

for required_file in \
  docker-compose.yml \
  configs/prometheus.yml \
  configs/prometheus/alert_rules.yml \
  configs/grafana/provisioning/datasources/prometheus.yml \
  configs/grafana/provisioning/dashboards/watchops.yml \
  configs/grafana/dashboards/watchops-lite.json \
  demo/logs/checkout_logs.jsonl \
  demo/knowledge/checkout_runbook.md \
  web/index.html \
  web/app.js \
  web/styles.css; do
  check_file "${required_file}"
done

printf '\nLocal ports and services (warnings are expected before startup)\n'
check_port "WatchOps-Lite" 8080
check_port "Redis" 6379
check_port "Elasticsearch" 9200
check_port "MySQL" 3306
check_port "Prometheus" 9090
check_port "Jaeger" 16686
check_port "Grafana" 3000

check_http "WatchOps-Lite" "http://127.0.0.1:8080/healthz"
check_http "Elasticsearch" "http://127.0.0.1:9200"
check_http "Prometheus" "http://127.0.0.1:9090/-/ready"
check_http "Jaeger" "http://127.0.0.1:16686"
check_http "Grafana" "http://127.0.0.1:3000/api/health"

printf '\nSummary: PASS=%d WARN=%d FAIL=%d\n' \
  "${PASS_COUNT}" "${WARN_COUNT}" "${FAIL_COUNT}"
if ((FAIL_COUNT > 0)); then
  exit 1
fi
exit 0
