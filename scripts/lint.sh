#!/usr/bin/env bash
# Runs every check on the code: formatting, linters and type checks for Go,
# the frontend, the Python model script and these shell scripts. It runs
# them all, then fails if any failed, so one run shows every problem.
#
#   scripts/lint.sh          check
#   scripts/lint.sh --fix    first apply every automatic fix, then check
#
# Tools: deps/tools (scripts/deps.sh) and frontend/node_modules (npm install).
# The Go checks compile the cgo packages, so CGO_ENABLED=1 and CC must be set
# as for a build. govulncheck needs the network (Go's vulnerability database).
set -uo pipefail
cd "$(dirname "$0")/.." || exit
T=deps/tools
GO=(. ./internal/... ./services/...) # not ./...: build/ holds other platforms' templates

for tool in golangci-lint deadcode govulncheck ruff shellcheck; do
  [ -x "$T/$tool.exe" ] || { echo "missing $T/$tool.exe: run scripts/deps.sh" >&2; exit 2; }
done
[ -d frontend/node_modules ] || { echo "missing frontend/node_modules: run npm install in frontend/" >&2; exit 2; }

if [ "${1:-}" = --fix ]; then
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
step "frontend: vue-tsc, eslint, prettier" npm --prefix frontend run -s check
step "python: ruff" bash -c "'$T/ruff' check --quiet && '$T/ruff' format --check --quiet"
step "shell: shellcheck" "$T/shellcheck" -x scripts/*.sh

if [ ${#failed[@]} -gt 0 ]; then
  printf 'failed: %s\n' "${failed[@]}" >&2
  exit 1
fi
echo "all checks passed"
