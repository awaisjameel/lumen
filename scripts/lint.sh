#!/usr/bin/env bash
# Runs static checks for Go, the frontend, the Python scripts, these shell
# scripts and the GitHub workflows, plus the frontend and installer-payload
# regression tests, the documentation's relative links, the dependencies'
# known vulnerabilities and the large-library benchmark.
# It runs them all, then fails if any failed, so one run shows every problem.
#
#   scripts/lint.sh                check
#   scripts/lint.sh --fix          first apply every automatic fix, then check
#   scripts/lint.sh --no-bench     leave out the latency budgets, which CI runs
#                                  as a job of its own on slower machines
#   scripts/lint.sh --no-go        leave out the Go checks, for a change with no
#                                  Go in it (CI decides with scripts/ci_scope.py)
#   scripts/lint.sh --no-vulncheck leave out govulncheck and npm audit, which
#                                  read a live advisory database: CI runs them
#                                  when dependencies change and every week
#
# Tools: deps/tools (scripts/deps.sh) and frontend/node_modules (npm ci).
# The Go checks compile the cgo packages, so CGO_ENABLED=1 and CC must be set
# as for a build. The vulnerability checks need the network.
set -uo pipefail
cd "$(dirname "$0")/.." || exit
T=deps/tools
GO=(. ./internal/... ./services/... ./cmd/... ./scripts/...) # not ./...: build/ holds other platforms' templates

for tool in golangci-lint deadcode govulncheck ruff shellcheck actionlint zizmor; do
  [ -x "$T/$tool.exe" ] || { echo "missing $T/$tool.exe: run scripts/deps.sh" >&2; exit 2; }
done
[ -d frontend/node_modules ] || { echo "missing frontend/node_modules: run npm ci in frontend/" >&2; exit 2; }

fix=0 bench=1 go=1 vuln=1
for arg in "$@"; do
  case $arg in
    --fix) fix=1 ;;
    --no-bench) bench=0 ;;
    --no-go) go=0 bench=0 ;;
    --no-vulncheck) vuln=0 ;;
    *) echo "usage: scripts/lint.sh [--fix] [--no-bench] [--no-go] [--no-vulncheck]" >&2; exit 2 ;;
  esac
done

if [ "$fix" -eq 1 ]; then
  echo "== fixing"
  if [ "$go" -eq 1 ]; then
    "$T/golangci-lint" fmt "${GO[@]}"
    "$T/golangci-lint" run --fix "${GO[@]}" >/dev/null
  fi
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

if [ "$go" -eq 1 ]; then
  step "go: golangci-lint (gofmt, goimports, vet, staticcheck and more)" "$T/golangci-lint" run "${GO[@]}"
  step "go: deadcode" bash -c "out=\$('$T/deadcode' -test ${GO[*]}) && [ -z \"\$out\" ] || { echo \"\$out\"; false; }"
fi
if [ "$go" -eq 1 ] && [ "$vuln" -eq 1 ]; then
  step "go: govulncheck" "$T/govulncheck" "${GO[@]}"
fi
if [ "$vuln" -eq 1 ]; then
  # High and critical advisories fail; moderate ones are left to Dependabot.
  step "npm: audit (high and critical)" bash -c "npm audit --prefix frontend --audit-level=high && npm audit --prefix e2e --audit-level=high"
fi
if [ "$bench" -eq 1 ]; then
  step "store: large-library latency budgets" bash scripts/bench.sh
fi
step "frontend: vue-tsc, eslint, prettier" npm --prefix frontend run -s check
step "frontend: regression tests" npm --prefix frontend run -s test
step "native harness: error attribution, exit and launch-wait regressions" node --test e2e/lib/errors.test.mjs e2e/lib/suite.test.mjs e2e/lib/webview.test.mjs
step "python: ruff" bash -c "'$T/ruff' check --quiet && '$T/ruff' format --check --quiet"
step "installer: payload cleanup regressions" python scripts/installer_payload_test.py
step "dependencies: warm extraction regressions" python scripts/dependency_cache_test.py
step "ci: change scope rules match the code" python scripts/ci_scope_test.py
step "runtime: every DLL import shipped or part of Windows" python scripts/runtime_imports.py
step "docs: links, anchors and screenshot integrity" bash scripts/check-docs.sh
step "shell: shellcheck" "$T/shellcheck" -x scripts/*.sh .githooks/*
# actionlint checks expressions, contexts, job graphs and (with shellcheck)
# every run: block; zizmor checks the workflows' security. Both work offline.
step "workflows: actionlint" "$T/actionlint" -shellcheck "$T/shellcheck.exe" -pyflakes=
step "workflows: zizmor" "$T/zizmor" --offline --no-progress .github

if [ ${#failed[@]} -gt 0 ]; then
  printf 'failed: %s\n' "${failed[@]}" >&2
  exit 1
fi
echo "all checks passed"
