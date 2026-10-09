# shellcheck shell=bash
# Sourced by deps.sh: these local tools never download anything at run time.
get https://github.com/libjxl/libjxl/releases/download/v0.12.0/jxl-x64-windows-static.zip \
  jxl-0.12.0.zip 3025d7e308390796d20492322e606bc92decaee7b6bc99d3f7547870ae5db7de
get https://github.com/libjpeg-turbo/libjpeg-turbo/releases/download/3.1.2/libjpeg-turbo-3.1.2-gcc-x64.exe \
  jpeg-3.1.2.exe ab82ae062bd277ffbf42baf866ba4019f139e9e84c87663c403a3c7d5dca192d
mkdir -p "$RT/compress" "$D/jpeg"
unzip -q -o -j "$D/cache/jxl-0.12.0.zip" '*/bin/cjxl.exe' '*/bin/djxl.exe' -d "$RT/compress"
# shellcheck source=scripts/oxipng-deps.sh
source "$ROOT/scripts/oxipng-deps.sh"
# Extract NSIS without installing it or changing the machine's PATH.
command -v 7z >/dev/null || { echo '7-Zip is required to extract the pinned compression tools' >&2; exit 1; }
7z x -y "-o$D/jpeg" "$D/cache/jpeg-3.1.2.exe" >/dev/null
cp "$D/jpeg/bin/jpegtran.exe" "$D/jpeg/bin/djpeg.exe" "$D/jpeg/bin/libjpeg-62.dll" "$RT/compress/"
cp "$D/jpeg/doc/LICENSE.md" "$D/licenses/jpeg-turbo.LICENSE.txt"
# The Windows archive carries the complete notices of its statically linked libraries.
unzip -p "$D/cache/jxl-0.12.0.zip" '*/licenses/LICENSE.*' >"$D/licenses/jxl.LICENSE.txt"
[ -s "$D/licenses/jxl.LICENSE.txt" ] || { echo 'JPEG XL notices missing' >&2; exit 1; }
