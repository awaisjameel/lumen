#!/usr/bin/env bash
# Turns on the repository's git hooks (.githooks): commit-msg checks the
# message, pre-commit the staged files' formatting and paths, pre-push the
# frontend check, Go vet and the documentation's links. Run once per clone.
set -euo pipefail
cd "$(dirname "$0")/.."
git config core.hooksPath .githooks
echo "git hooks on: $(git config core.hooksPath) (commit-msg, pre-commit, pre-push)"
