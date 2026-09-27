# shellcheck shell=bash
# Source this (Git Bash) before go test, go vet or go run: Lumen links
# libvips-42.dll, which Windows must find when a binary starts, so
# deps/runtime goes on PATH. (Built apps find it beside Lumen.exe.)
LUMEN_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
export PATH="$LUMEN_ROOT/deps/runtime:$PATH"
