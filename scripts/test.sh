#!/usr/bin/env sh
# Run every package test from the repo root. Requires Go on PATH.
# Usage: ./scripts/test.sh
set -eu
cd "$(dirname "$0")/.."

echo "==> go test + coverage"
go test ./... -count=1 -cover

echo "==> go vet"
go vet ./...

echo "ok"
