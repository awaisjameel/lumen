#!/usr/bin/env bash
# The large-library gate uses three warmed calls per figure and one scratch seed.
# Budgets are reviewed ceilings; raise them by hand with repeat measurements and
# a reason in review, never by copying the latest result into the budget file.
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=scripts/env.sh
source scripts/env.sh

if [ "$#" -ne 0 ]; then
  echo "usage: scripts/bench.sh (budgets are edited by hand in review)" >&2
  exit 2
fi

results=$(mktemp)
trap 'rm -f "$results"' EXIT

go test -count=1 -timeout 60s ./scripts/benchgate
status=0
go test -run '^$' -bench '^BenchmarkLargeLibraryStore$' -benchtime=3x -count=1 \
  -timeout 900s ./internal/store | tee "$results" || status=$?
go run ./scripts/benchgate < "$results" || status=1
exit "$status"
