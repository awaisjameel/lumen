#!/usr/bin/env bash
# The pre-commit hook's checks, on the staged content of each staged file (not
# the working tree, so a partly staged file is judged as it will be
# committed). Fast enough to run on every commit; scripts/lint.sh stays the
# full gate.
#
#   - Go files are gofmt-formatted.
#   - Frontend files are Prettier-formatted (frontend/node_modules).
#   - Python files are Ruff-formatted (deps/tools).
#   - No added line names this checkout's or this user's absolute path.
set -euo pipefail
cd "$(dirname "$0")/.."

mapfile -d '' staged < <(git diff --cached --name-only --diff-filter=ACMR -z)
[ ${#staged[@]} -gt 0 ] || exit 0
failed=0
# fail MESSAGE: report one problem; the hook fails once every check has run.
fail() {
  echo "pre-commit: $1" >&2
  failed=1
}

# formatted NAME FORMATTER...: whether the staged NAME is unchanged by the
# formatter, which reads the file on stdin and writes it to stdout.
formatted() {
  local name=$1
  shift
  cmp -s <(git show ":$name") <(git show ":$name" | "$@" 2>/dev/null)
}

prettier=frontend/node_modules/.bin/prettier
ruff=deps/tools/ruff.exe
# prettier_formatted NAME: whether the staged NAME is unchanged by Prettier,
# run from frontend/ so its config and .prettierignore apply (an ignored file
# comes back unchanged).
prettier_formatted() {
  cmp -s <(git show ":$1") <(git show ":$1" | (cd frontend && node_modules/.bin/prettier --stdin-filepath "${1#frontend/}") 2>/dev/null)
}
for name in "${staged[@]}"; do
  case $name in
    frontend/bindings/* | frontend/dist/*) ;;
    *.go)
      formatted "$name" gofmt || fail "$name is not gofmt-formatted (gofmt -w $name)"
      ;;
    frontend/*.ts | frontend/*.vue | frontend/*.js | frontend/*.mjs | frontend/*.json | frontend/*.css | frontend/*.html | frontend/*.md)
      if [ -x "$prettier" ]; then
        prettier_formatted "$name" || fail "$name is not Prettier-formatted (npm --prefix frontend run format)"
      fi
      ;;
    *.py)
      if [ -x "$ruff" ]; then
        formatted "$name" "$ruff" format --stdin-filename "$name" - || fail "$name is not Ruff-formatted ($ruff format $name)"
      fi
      ;;
  esac
done
[ -x "$prettier" ] || echo "pre-commit: Prettier skipped (run npm ci in frontend/)" >&2
[ -x "$ruff" ] || echo "pre-commit: Ruff skipped (run scripts/deps.sh)" >&2

# Paths of this machine never belong in the repository: write paths relative
# to the repository, or <scratch> for a scratch folder.
local_paths=()
for dir in "$PWD" "$HOME"; do
  local_paths+=("$dir")
  command -v cygpath >/dev/null && local_paths+=("$(cygpath -w "$dir")" "$(cygpath -m "$dir")")
done
added=$(git diff --cached -U0 --no-color --diff-filter=ACMR | sed -n '/^+++/d; /^+/p')
# Windows paths are case-insensitive, so compare in lower case.
added=${added,,}
for dir in "${local_paths[@]}"; do
  if [[ $added == *"${dir,,}"* ]]; then
    fail "an added line names the local path $dir; use a repository-relative path"
  fi
done

if [ "$failed" -ne 0 ]; then
  echo "pre-commit: fix the above, stage the result and commit again" >&2
fi
exit "$failed"
