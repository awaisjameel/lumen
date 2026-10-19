#!/usr/bin/env bash
# Prepares and tags releases. Lumen's version is set once, in build/config.yml.
#
#   scripts/version.sh bump X.Y.Z[-pre]   set the version, regenerate the build
#                                         assets, check them and commit, on a
#                                         release/vX.Y.Z branch when run on main
#   scripts/version.sh tag                tag main's version as vX.Y.Z and push
#                                         the tag, which starts the release
#                                         workflow (.github/workflows/release.yml)
#
# Needs the build toolchain (README, "Building from source"): bump runs Wails
# and a Go test.
set -euo pipefail
cd "$(dirname "$0")/.."

die() {
  echo "version: $*" >&2
  exit 1
}
current() { sed -n 's/^  version: "\([^"]*\)".*/\1/p' build/config.yml; }
clean() { git diff --quiet && git diff --cached --quiet; }

bump() {
  local new=$1 old
  old=$(current)
  [[ $new =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || die "$new is not a version (X.Y.Z or X.Y.Z-pre)"
  [ "$new" != "$old" ] && [ "$(printf '%s\n%s\n' "$old" "$new" | sort -V | tail -n 1)" = "$new" ] ||
    die "$new does not follow the current version $old"
  clean || die "commit or stash your changes first"
  if [ "$(git branch --show-current)" = main ]; then
    git switch -c "release/v$new"
  fi
  sed -i "s/^\(  version: \"\)[^\"]*\(\".*\)$/\1$new\2/" build/config.yml
  [ "$(current)" = "$new" ] || die "could not set the version in build/config.yml"
  wails3 task common:update:build-assets
  # The task resets the Linux package's licence to Wails' template default.
  sed -i 's/^license: .*/license: "PolyForm-Noncommercial-1.0.0"/' build/linux/nfpm/nfpm.yaml
  # shellcheck source=scripts/env.sh
  source scripts/env.sh
  go test -count=1 -timeout 900s -run '^(TestVersionsAgree|TestLicenceAndCopyrightAgree)$' .
  git add -A build
  git commit -q -m "chore(release): Lumen $new"
  echo "version: $old -> $new committed on $(git branch --show-current)."
  echo "Next: push the branch, open a pull request into main, merge it, then run scripts/version.sh tag on main."
}

tag() {
  local version
  [ "$(git branch --show-current)" = main ] || die "switch to main first: releases are tagged from main"
  clean || die "main has uncommitted changes"
  git fetch -q origin main --tags
  [ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || die "main is not origin/main: pull or push first"
  version=$(current)
  [ -n "$version" ] || die "build/config.yml has no version"
  git rev-parse -q --verify "refs/tags/v$version" >/dev/null && die "v$version is already tagged; bump the version first"
  [ -z "$(git ls-remote --tags origin "refs/tags/v$version")" ] || die "origin already has v$version"
  git tag -a "v$version" -m "Lumen $version"
  git push origin "v$version"
  echo "version: pushed v$version. The release workflow builds it into a draft release:"
  echo "  https://github.com/awaisjameel/lumen/actions/workflows/release.yml"
}

case "${1:-} $#" in
  "bump 2") bump "$2" ;;
  "tag 1") tag ;;
  *)
    echo "usage: scripts/version.sh bump X.Y.Z[-pre] | tag" >&2
    exit 2
    ;;
esac
