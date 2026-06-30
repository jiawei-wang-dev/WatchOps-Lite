#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

unformatted="$(gofmt -l cmd internal)"
if [[ -n "${unformatted}" ]]; then
  echo "Go files require formatting:" >&2
  echo "${unformatted}" >&2
  exit 1
fi

module_hash_before="$(cksum go.mod go.sum)"
go mod tidy
module_hash_after="$(cksum go.mod go.sum)"
if [[ "${module_hash_before}" != "${module_hash_after}" ]]; then
  echo "go mod tidy changed go.mod or go.sum; commit the normalized files." >&2
  exit 1
fi

go test ./...
go vet ./...
git diff --check

echo "WatchOps-Lite verification passed."
