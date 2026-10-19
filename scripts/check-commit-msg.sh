#!/usr/bin/env bash
# Checks commit messages against the repository's two rules: the subject is a
# Conventional Commit, and no message credits or mentions an AI assistant
# (AGENTS.md, "Git rules"). The commit-msg hook and CI both run it.
#
#   scripts/check-commit-msg.sh FILE           one message, as the hook gets it
#   scripts/check-commit-msg.sh --range A..B   every non-merge commit in a range
set -euo pipefail

TYPES='feat|fix|perf|refactor|docs|test|build|ci|chore|style|revert'
SUBJECT="^(${TYPES})(\([a-z0-9][a-z0-9/-]*\))?!?: [^ ]"
# The subjects git writes itself, for merges, reverts and autosquash fixups.
GIT_SUBJECT='^(Merge |Revert "|(fixup|squash|amend)! )'
ASSISTANT='claude|codex|copilot|chatgpt|gpt|gemini|anthropic|openai'

# check NAME: read one message on stdin, say what is wrong with it, and
# return 1 if anything is.
check() {
  local name=$1 message subject status=0
  # Comments and everything below the scissors line never reach the commit.
  message=$(sed -e '/^# -* >8 -*$/,$d' -e '/^#/d')
  subject=$(sed -n '/[^[:space:]]/{p;q;}' <<<"$message")
  if ! [[ $subject =~ $SUBJECT || $subject =~ $GIT_SUBJECT ]]; then
    echo "$name: the subject must read type(scope): summary, with type one of ${TYPES//|/, }; got: $subject" >&2
    status=1
  fi
  # CLAUDE.md is a file's name, not a mention.
  local words=${message//CLAUDE.md/}
  if grep -qiwE "$ASSISTANT" <<<"$words"; then
    echo "$name: commit messages never credit or mention an AI assistant (AGENTS.md):" >&2
    grep -iwE "$ASSISTANT" <<<"$words" | sed 's/^/  /' >&2
    status=1
  fi
  return "$status"
}

if [ "${1:-}" = --range ] && [ $# -eq 2 ]; then
  failed=0
  while read -r sha; do
    git log -1 --format=%B "$sha" | check "${sha:0:10}" || failed=1
  done < <(git rev-list --no-merges "$2")
  exit "$failed"
fi
if [ $# -ne 1 ] || [ ! -f "$1" ]; then
  echo "usage: scripts/check-commit-msg.sh FILE | --range A..B" >&2
  exit 2
fi
check "commit message" <"$1"
