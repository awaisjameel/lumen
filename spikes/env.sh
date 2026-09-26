# Source this before building or running spikes (Git Bash).
source $TEMP/lumen-env.sh
S=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
export PATH="$S/.cache/vips-dev-8.18/bin:$S/.cache/lib:$PATH"
# sqlite-vec's cgo binding includes "sqlite3.h"; mattn ships it as sqlite3-binding.h.
export CGO_CFLAGS="-O2 -I$(cygpath -m "$S/.cache/sqlite-include") -I$(cygpath -m "$(go env GOMODCACHE)")/github.com/mattn/go-sqlite3@v1.14.52"
