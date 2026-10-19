#!/usr/bin/env bash
# Runs static checks for Go, the frontend, the Python model script and these
# shell scripts, plus frontend regression tests and the large-library benchmark.
# It runs them all, then fails if any failed, so one run shows every problem.
#
#   scripts/lint.sh             check
#   scripts/lint.sh --fix       first apply every automatic fix, then check
#   scripts/lint.sh --no-bench  leave out the latency budgets, which CI runs
#                               as a step of their own on slower machines
#
# Tools: deps/tools (scripts/deps.sh) and frontend/node_modules (npm ci).
# The Go checks compile the cgo packages, so CGO_ENABLED=1 and CC must be set
# as for a build. govulncheck needs the network (Go's vulnerability database).
set -uo pipefail
cd "$(dirname "$0")/.." || exit
T=deps/tools
GO=(. ./internal/... ./services/... ./cmd/... ./scripts/...) # not ./...: build/ holds other platforms' templates

for tool in golangci-lint deadcode govulncheck ruff shellcheck; do
  [ -x "$T/$tool.exe" ] || { echo "missing $T/$tool.exe: run scripts/deps.sh" >&2; exit 2; }
done
[ -d frontend/node_modules ] || { echo "missing frontend/node_modules: run npm ci in frontend/" >&2; exit 2; }

fix=0 bench=1
for arg in "$@"; do
  case $arg in
    --fix) fix=1 ;;
    --no-bench) bench=0 ;;
    *) echo "usage: scripts/lint.sh [--fix] [--no-bench]" >&2; exit 2 ;;
  esac
done

if [ "$fix" -eq 1 ]; then
  echo "== fixing"
  "$T/golangci-lint" fmt "${GO[@]}"
  "$T/golangci-lint" run --fix "${GO[@]}" >/dev/null
  (cd frontend && npx --no-install eslint . --fix >/dev/null; npm run -s format >/dev/null)
  "$T/ruff" check --fix --quiet
  "$T/ruff" format --quiet
fi

failed=()
# step NAME COMMAND...: run one check and remember whether it failed.
step() {
  local name=$1
  shift
  echo "== $name"
  "$@" || failed+=("$name")
}

step "go: golangci-lint (gofmt, goimports, vet, staticcheck and more)" "$T/golangci-lint" run "${GO[@]}"
step "go: deadcode" bash -c "out=\$('$T/deadcode' -test ${GO[*]}) && [ -z \"\$out\" ] || { echo \"\$out\"; false; }"
step "go: govulncheck" "$T/govulncheck" "${GO[@]}"
if [ "$bench" -eq 1 ]; then
  step "store: large-library latency budgets" bash scripts/bench.sh
fi
step "frontend: vue-tsc, eslint, prettier" npm --prefix frontend run -s check
step "frontend: regression tests" npm --prefix frontend run -s test
step "python: ruff" bash -c "'$T/ruff' check --quiet && '$T/ruff' format --check --quiet"
step "installer: payload cleanup regressions" python scripts/installer_payload_test.py
step "docs: relative links" python scripts/check_links.py
step "shell: shellcheck" "$T/shellcheck" -x scripts/*.sh .githooks/*

if [ ${#failed[@]} -gt 0 ]; then
  printf 'failed: %s\n' "${failed[@]}" >&2
  exit 1
fi
echo "all checks passed"
