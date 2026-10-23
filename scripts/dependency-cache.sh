# shellcheck shell=bash
# Extract a pinned archive again when its hash changes, including on a warm
# checkout. Stage it first so a damaged archive cannot remove a working copy.
unpack_dependency_zip() {
  local archive=$1 destination=$2 archive_root=$3 fingerprint=$4 staging
  if [ "$(cat "$destination/.lumen-archive" 2>/dev/null)" = "$fingerprint" ]; then return; fi
  staging=$(mktemp -d "$destination.unpack.XXXXXX") || return
  if ! unzip -q "$archive" -d "$staging" || [ ! -d "$staging/$archive_root" ]; then
    rm -rf "$staging"
    return 1
  fi
  rm -rf "$destination" || { rm -rf "$staging"; return 1; }
  mv "$staging/$archive_root" "$destination" || { rm -rf "$staging"; return 1; }
  rm -rf "$staging"
  printf '%s\n' "$fingerprint" >"$destination/.lumen-archive"
}
