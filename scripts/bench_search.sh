#!/usr/bin/env bash
# Aspirational FTS5 bench — times MATCH over a throwaway in-memory DB.
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="${PATH}:/usr/local/go/bin"
exec go test ./internal/search/ -bench=BenchmarkSearch -benchtime=2s -count=1
