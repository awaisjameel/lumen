# shellcheck shell=bash
# Source this before building or running spikes (Git Bash), with the build
# toolchain set up as docs/development.md describes.
S=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/env.sh
source "$S/../scripts/env.sh"
export PATH="$S/.cache/vips-dev-8.18/bin:$S/.cache/lib:$PATH"
# sqlite-vec's cgo binding includes "sqlite3.h"; mattn ships it as sqlite3-binding.h.
include=$(cygpath -m "$S/.cache/sqlite-include")
modcache=$(cygpath -m "$(go env GOMODCACHE)")
export CGO_CFLAGS="-O2 -I$include -I$modcache/github.com/mattn/go-sqlite3@v1.14.52"
