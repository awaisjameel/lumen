#!/usr/bin/env bash
# Fetches and prepares everything Lumen needs besides Go and Node: libvips,
# ONNX Runtime with DirectML and the Visual C++ runtime it needs, FFmpeg, the
# SigLIP 2, face and OCR models, and the linters. Every download is pinned and
# checked against its SHA-256 (the two values model prep reads from the SigLIP
# checkpoint are pinned in prepare_models.py; its Python packages are pinned in
# model-requirements.txt). Run from Git Bash; safe to re-run (finished steps are
# skipped). Needs curl, unzip, 7-Zip, Go and Python 3.15 (model prep only).
#
#   deps/cache/     downloads
#   deps/vips/      libvips headers and import library (for cgo)
#   deps/runtime/   exactly what ships beside Lumen.exe (DLLs, ffmpeg/, models/)
#   deps/licenses/  the licence texts of what ships, for scripts/notices.py
#   deps/installer/ what only the installer carries: the WebView2 runtime
#   deps/tools/     development tools: golangci-lint, deadcode, govulncheck, ruff, shellcheck, NSIS
#
# The last step copies deps/runtime into bin/ (hard links, so it's instant).
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$PWD
D=$ROOT/deps
RT=$D/runtime
mkdir -p "$D/cache" "$RT/ffmpeg" "$D/src/siglip2" "$D/src/faces" "$D/licenses"
# shellcheck source=scripts/dependency-cache.sh
source "$ROOT/scripts/dependency-cache.sh"

# get URL FILE SHA256: download to deps/cache/FILE unless it is already there
# with the right hash.
get() {
  local url=$1 out=$D/cache/$2 sum=$3
  if [ -f "$out" ] && echo "$sum *$out" | sha256sum -c --status; then return; fi
  echo "fetch $2"
  curl -fL --retry 3 --progress-bar -o "$out.part" "$url"
  echo "$sum *$out.part" | sha256sum -c --status || { echo "checksum mismatch: $2" >&2; rm -f "$out.part"; exit 1; }
  mv "$out.part" "$out"
}

# libvips 8.18.7, "web" formats (JPEG, PNG, WebP, GIF, TIFF, AVIF, SVG),
# statically linked into one DLL. It has no HEVC decoder: HEIC goes through WIC or FFmpeg.
VIPS_SHA=a736af61d3b46499760efa606e469cb14e060445b1f6fd36a1fa2e75009e1197
get https://github.com/libvips/build-win64-mxe/releases/download/v8.18.7/vips-dev-x64-web-8.18.7-static.zip \
  vips-8.18.7.zip "$VIPS_SHA"
unpack_dependency_zip "$D/cache/vips-8.18.7.zip" "$D/vips" vips-dev-8.18 "$VIPS_SHA"
# Archive timestamps need not increase between releases; copy the selected build.
cp "$D/vips/bin/libvips-42.dll" "$RT/"
cp "$D/vips/LICENSE" "$D/licenses/libvips.LICENSE.txt"
cp "$D/vips/versions.json" "$D/licenses/libvips.versions.json" # the libraries built into the DLL

# ONNX Runtime 1.24.4, the last release with a DirectML build (onnxruntime_go
# v1.27.0 matches it), and DirectML 1.15.4.
get https://api.nuget.org/v3-flatcontainer/microsoft.ml.onnxruntime.directml/1.24.4/microsoft.ml.onnxruntime.directml.1.24.4.nupkg \
  ort-dml.nupkg 57e9f11b73437bef7a309496135d4c1f96b1a8e9ddba60013fa27bfc1d788681
get https://api.nuget.org/v3-flatcontainer/microsoft.ai.directml/1.15.4/microsoft.ai.directml.1.15.4.nupkg \
  directml.nupkg 4e7cb7ddce8cf837a7a75dc029209b520ca0101470fcdf275c1f49736a3615b9
[ -f "$RT/onnxruntime.dll" ] || unzip -q -o -j "$D/cache/ort-dml.nupkg" 'runtimes/win-x64/native/*.dll' -d "$RT"
[ -f "$RT/DirectML.dll" ] || unzip -q -o -j "$D/cache/directml.nupkg" 'bin/x64-win/DirectML.dll' -d "$RT"
unzip -p "$D/cache/ort-dml.nupkg" LICENSE >"$D/licenses/onnxruntime.LICENSE.txt"
unzip -p "$D/cache/ort-dml.nupkg" ThirdPartyNotices.txt >"$D/licenses/onnxruntime.ThirdPartyNotices.txt"
unzip -p "$D/cache/directml.nupkg" LICENSE.txt >"$D/licenses/directml.LICENSE.txt"
unzip -p "$D/cache/directml.nupkg" ThirdPartyNotices.txt >"$D/licenses/directml.ThirdPartyNotices.txt"

# The Visual C++ runtime ONNX Runtime links against. Windows does not include
# it, and installing Microsoft's redistributable needs elevation, so its DLLs
# ship beside Lumen.exe (app-local deployment), where Windows finds them before
# System32. The redistributable is a WiX bundle: the cabinet after its UI holds
# the x64 runtime's cabinet (a4), whose entries are named <dll>_amd64. The
# offset and size are fixed by the pinned hash; nothing is installed or run.
VCRT=14.51.36247.0
get https://download.visualstudio.microsoft.com/download/pr/ebdab8e5-1d7b-4d9f-a11b-cbb1720c3b12/843068991DAAA1F73AD9F6239BCE4D0F6A07A51F18C37EA2A867E9BECA71295C/VC_redist.x64.exe \
  vc-redist-$VCRT-x64.exe 843068991daaa1f73ad9f6239bce4d0f6a07a51f18c37ea2a867e9beca71295c
VCRT_SUMS="7c26614e1d733892c2deac7e245ce115504b1d80592dd0a01b08e3e5a55f89ca *$RT/msvcp140.dll
206c931bf90fdad8816de3b5e2ef80b2bcaa9406c89ecc05fe6fddffe251e982 *$RT/msvcp140_1.dll
d1f4225df2cd877dbf130d5668a021dce3f94118455ff5ec952061c30afc9ce7 *$RT/vcruntime140.dll
a7146c08f89fe5b04541ab507cdb59ff7b44534d4ba3c668a426c6450a03434e *$RT/vcruntime140_1.dll"
if ! sha256sum -c --quiet --status <<<"$VCRT_SUMS" 2>/dev/null; then
  command -v 7z >/dev/null || { echo '7-Zip is required to extract the pinned Visual C++ runtime' >&2; exit 1; }
  rm -rf "$D/vcrt" && mkdir -p "$D/vcrt"
  tail -c +630001 "$D/cache/vc-redist-$VCRT-x64.exe" | head -c 18091661 >"$D/vcrt/payload.cab"
  7z e -y "-o$D/vcrt" "$D/vcrt/payload.cab" a4 >/dev/null
  for dll in msvcp140 msvcp140_1 vcruntime140 vcruntime140_1; do
    7z e -y -so "$D/vcrt/a4" "$dll.dll_amd64" >"$RT/$dll.dll"
  done
  rm -rf "$D/vcrt"
  sha256sum -c --quiet <<<"$VCRT_SUMS"
fi

# FFmpeg 9.0 (LGPL, shared). Only ffmpeg.exe, ffprobe.exe and the libraries
# they load ship. BtbN deletes its daily autobuilds after about two weeks but
# keeps each month's last one, so pin a month-end autobuild (and its hash).
FFMPEG_SHA=7157177b8a6cb2174c1650ba8c71b363f2c78cba5330f88c4c02cf5b2b880646
get https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-09-30-13-08/ffmpeg-n9.0.2-17-g2a571b6068-win64-lgpl-shared-9.0.zip \
  ffmpeg.zip $FFMPEG_SHA
# A new pin replaces the whole folder, so no library of the old build stays.
# The archive's hash is noted outside the runtime, which ships as it is.
if [ "$(cat "$D/ffmpeg.extracted" 2>/dev/null)" != "$FFMPEG_SHA" ]; then
  rm -rf "$RT/ffmpeg"
  unzip -q -o -j "$D/cache/ffmpeg.zip" '*/bin/ffmpeg.exe' '*/bin/ffprobe.exe' '*/bin/*.dll' '*/LICENSE.txt' -d "$RT/ffmpeg"
  echo "$FFMPEG_SHA" >"$D/ffmpeg.extracted"
fi
cp -u "$RT/ffmpeg/LICENSE.txt" "$D/licenses/ffmpeg.LICENSE.txt"

# LibRaw 0.22.2, for camera RAW, used under its CDDL-1.0 option. It is built
# here from its unmodified source with the same llvm-mingw clang cgo uses
# (LibRaw's own Windows binaries need the Visual C++ runtime), and linked
# statically into lumen-raw.exe, the worker in native/lumen-raw. No optional
# library is compiled in, so lossy-JPEG and deflate-compressed DNG are not read.
get https://www.libraw.org/data/LibRaw-0.22.2.tar.gz \
  libraw-0.22.2.tar.gz de86b035655accff8d4010f1a221fdf50d353cb7b1422ba26f14a0db92612cfa
LR=$D/libraw/LibRaw-0.22.2
if [ ! -f "$LR/libraw/libraw.h" ]; then
  rm -rf "$D/libraw"
  mkdir -p "$D/libraw" && tar -xzf "$D/cache/libraw-0.22.2.tar.gz" -C "$D/libraw"
fi
cp -u "$LR/LICENSE.CDDL" "$D/licenses/libraw.LICENSE.CDDL.txt"
cp -u "$LR/COPYRIGHT" "$D/licenses/libraw.COPYRIGHT.txt"
RAW_FLAGS=(-O2 -I"$LR" -DLIBRAW_NODLL -DLIBRAW_NOTHREADS)
# libraw_object SOURCE: compile one LibRaw source file unless its object is newer.
libraw_object() {
  local obj=$D/libraw/obj/${1//\//_}.o
  [ "$obj" -nt "$LR/$1" ] || clang++ -c -w "${RAW_FLAGS[@]}" -o "$obj" "$LR/$1"
}
# The compiler's identity, not where it is unpacked, decides a rebuild.
RAW_BUILD=$( { clang++ --version | sed '/^InstalledDir:/d'; sha256sum "$ROOT/native/lumen-raw/main.cpp"; printf '%s\n' "${RAW_FLAGS[@]}"; } | sha256sum | cut -d' ' -f1)
if [ ! -f "$RT/lumen-raw.exe" ] || [ "$(cat "$D/libraw/worker.build" 2>/dev/null)" != "$RAW_BUILD" ]; then
  echo "build lumen-raw.exe"
  # Objects from an earlier compiler or LibRaw release must not be reused.
  rm -rf "$D/libraw/obj"
  mkdir -p "$D/libraw/obj"
  # The *_ph.cpp files are placeholders for the parts they stand in for.
  mapfile -t RAW_SOURCES < <(cd "$LR" && find src -name '*.cpp' ! -name '*_ph.cpp' | sort)
  for src in "${RAW_SOURCES[@]}"; do
    libraw_object "$src" &
    while [ "$(jobs -rp | wc -l)" -ge 8 ]; do wait -n; done
  done
  wait
  [ "$(find "$D/libraw/obj" -name '*.o' | wc -l)" -eq "${#RAW_SOURCES[@]}" ] || { echo "LibRaw did not compile" >&2; exit 1; }
  clang++ -std=c++17 -municode -static -Wall -Wextra "${RAW_FLAGS[@]}" \
    -o "$RT/lumen-raw.exe" "$ROOT/native/lumen-raw/main.cpp" "$D"/libraw/obj/*.o -lws2_32
  printf '%s\n' "$RAW_BUILD" >"$D/libraw/worker.build"
fi

# SigLIP 2 base (Apache-2.0): fp16 ONNX towers and the Gemma tokenizer.
HF=https://huggingface.co/onnx-community/siglip2-base-patch16-224-ONNX/resolve/ba1f3b0843f24bc5417d38e19c37b287d719b2f4
get $HF/tokenizer.json siglip2-tokenizer.json cb9140fae3ac5122c972d37adf83e1248471a38147ad76f8215c8872c6fd8322
get $HF/onnx/vision_model_fp16.onnx siglip2-vision-fp16.onnx a1959f7bd3993a607e48839f6d01e25b876fe76afda301b028b78eef68aabd95
get $HF/onnx/text_model_fp16.onnx siglip2-text-fp16.onnx 711da56ada0a4aa11c7dd3320df741081a3cae4f0ae1b5e5c6d5b294738d0eb0
ln -f "$D/cache/siglip2-tokenizer.json" "$D/src/siglip2/tokenizer.json"
ln -f "$D/cache/siglip2-vision-fp16.onnx" "$D/src/siglip2/vision_model_fp16.onnx"
ln -f "$D/cache/siglip2-text-fp16.onnx" "$D/src/siglip2/text_model_fp16.onnx"

# Faces: YuNet detector (MIT) and SFace recognizer (Apache-2.0), OpenCV Zoo.
HF=https://huggingface.co/opencv
YUNET=$HF/face_detection_yunet/resolve/3cc26e7f1014a5ee5d74a42acee58bafc9d0a310
SFACE=$HF/face_recognition_sface/resolve/3d7082438a6e4551e840c9b2bb60b71e8da4b524
get $YUNET/face_detection_yunet_2023mar.onnx face-yunet.onnx 8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4
get $YUNET/LICENSE face-yunet.LICENSE c83b8120c50ccbd4c4f96edf53141bdd566ebb8f8e9227e415326aa1b1aba958
get $SFACE/face_recognition_sface_2021dec.onnx face-sface.onnx 0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79
get $SFACE/LICENSE face-sface.LICENSE cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30
for f in yunet.onnx yunet.LICENSE sface.onnx sface.LICENSE; do ln -f "$D/cache/face-$f" "$D/src/faces/$f"; done
cp -u "$D/cache/face-yunet.LICENSE" "$D/licenses/yunet.LICENSE.txt"
cp -u "$D/cache/face-sface.LICENSE" "$D/licenses/sface.LICENSE.txt" # Apache-2.0, as SigLIP 2's and PP-OCRv5's

# OCR (an optional add-on, off by default): the PP-OCRv5 mobile text detector
# and the English PP-OCRv5 recogniser in ONNX, both Apache-2.0 (their model
# cards, kept as the licence notice, say so). Phase 0 measured exactly these
# files (spikes/README.md). They are used as they are, with no preparation.
DET=https://huggingface.co/PaddlePaddle/PP-OCRv5_mobile_det_onnx/resolve/e6f4fa85f00e168c862bc462aebca69eef9b3d3d
REC=https://huggingface.co/monkt/paddleocr-onnx/resolve/7b02d0a30a07ba2b92ad1ff5a8941ae2c633de65
get $DET/inference.onnx ocr-det.onnx a431985659dc921974177a95adcfbb90fd9e51989a5e04d70d0b75f597b6e61d
get $DET/inference.yml ocr-det.yml 98069072e1b6b37d727fd9d9f11725faa46d6ea0de012f2ed26caea011c37699
get $DET/README.md ocr-det.README.md 951c155673ac6c15feb85857c9024a62266e5956d15319ce578a8f508f399d66
get $REC/languages/english/rec.onnx ocr-rec.onnx 4e16deb22c4da6468bdca539b2cd3c8687825538b67109177c47d359ab994cd7
get $REC/languages/english/dict.txt ocr-rec.dict.txt e025a66d31f327ba0c232e03f407ae8d105e1e709e7ccb3f408aa778c24e70d6
get $REC/languages/english/config.json ocr-rec.json c7decf6d184bd44ed5dca9a5e7e4bd208d0bd9747e6d9d284de9e0bee2d22f6a
get $REC/README.md ocr-rec.README.md 783815b2d4127bbdf2847fac50d4dfa3b4ec58508324e43a7b41de66994f6611
mkdir -p "$RT/models/ocr"
for f in det.onnx det.yml det.README.md rec.onnx rec.dict.txt rec.json rec.README.md; do
  ln -f "$D/cache/ocr-$f" "$RT/models/ocr/$f"
done

# prepare MODEL: scripts/prepare_models.py turns deps/src/MODEL into
# deps/runtime/models/MODEL, again whenever the script changes.
prepare() {
  local out=$RT/models/$1
  [ -f "$out/$1.json" ] && [ ! "$ROOT/scripts/prepare_models.py" -nt "$out/$1.json" ] &&
    [ ! "$ROOT/scripts/model-requirements.txt" -nt "$out/$1.json" ] && return
  local venv=$D/venv-3.15
  if [ ! -x "$venv/Scripts/python" ]; then
    python -m venv "$venv"
  fi
  if ! cmp -s "$ROOT/scripts/model-requirements.txt" "$venv/.lumen-requirements"; then
    "$venv/Scripts/python" -m pip install -q -r "$ROOT/scripts/model-requirements.txt"
    cp "$ROOT/scripts/model-requirements.txt" "$venv/.lumen-requirements"
  fi
  PYTHONIOENCODING=utf-8 "$venv/Scripts/python" "$ROOT/scripts/prepare_models.py" "$1" "$D/src/$1" "$out"
}
prepare siglip2
prepare faces

# Development tools (never shipped): the linters scripts/lint.sh runs, into
# deps/tools. golangci-lint is built with this machine's Go (go install
# checks it against the Go checksum database), so it understands the Go
# version the project uses.
T=$D/tools
mkdir -p "$T"
get https://github.com/astral-sh/ruff/releases/download/0.17.2/ruff-x86_64-pc-windows-msvc.zip \
  ruff-0.17.2.zip 6f4de58abd7c8de2e8702a89fc0f221e6ca858ad23e25c658c707d83a48c79a0
"$T/ruff.exe" --version 2>/dev/null | grep -q '^ruff 0.17.2$' || unzip -q -o -j "$D/cache/ruff-0.17.2.zip" ruff.exe -d "$T"
get https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.zip \
  shellcheck-v0.11.0.zip 8a4e35ab0b331c85d73567b12f2a444df187f483e5079ceffa6bda1faa2e740e
"$T/shellcheck.exe" --version 2>/dev/null | grep -q '^version: 0.11.0$' ||
  unzip -q -o -j "$D/cache/shellcheck-v0.11.0.zip" shellcheck.exe -d "$T"
# goinstall BIN MODULE@VERSION: build a Go tool into deps/tools unless that
# version is already there.
goinstall() {
  local bin=$T/$1.exe pkg=$2 want=${2##*@}
  local metadata buildgo
  buildgo=$(go env GOVERSION)
  if [ -f "$bin" ]; then
    metadata=$(go version -m "$bin")
    # Analysis tools read the compiler's export format, which can change
    # in a patch release too. Rebuild when the project toolchain changes.
    if grep -q "^[[:space:]]mod[[:space:]].*[[:space:]]${want}[[:space:]]" <<<"$metadata" &&
      head -n 1 <<<"$metadata" | grep -q "${buildgo}$"; then
      return
    fi
  fi
  echo "build $1 $want"
  GOBIN=$T CGO_ENABLED=0 go install "$pkg"
}
goinstall golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
goinstall deadcode golang.org/x/tools/cmd/deadcode@v0.52.0
goinstall govulncheck golang.org/x/vuln/cmd/govulncheck@v1.9.0

# The installer (task package): NSIS 3.13 builds it, and Microsoft's
# standalone WebView2 runtime installer goes inside it, so a PC without
# WebView2 needs no internet to install Lumen. Microsoft's link
# (https://go.microsoft.com/fwlink/?linkid=2124701) always serves the newest
# runtime; the file pinned here is the one it served on 23 October 2026. When
# Microsoft retires it, fetch the link, check the file is signed by
# Microsoft (PowerShell: Get-AuthenticodeSignature), and pin its URL and hash.
get https://downloads.sourceforge.net/project/nsis/NSIS%203/3.13/nsis-3.13.zip \
  nsis-3.13.zip ba63dffc4410ee89193e1cb5a41989991bd77c61068da17e3156d136b7b0b3d8
if ! "$T/nsis/makensis.exe" -VERSION 2>/dev/null | grep -q '^v3.13$'; then
  rm -rf "$T/nsis" "$T/nsis-3.13"
  unzip -q "$D/cache/nsis-3.13.zip" -d "$T" && mv "$T/nsis-3.13" "$T/nsis"
fi
WV2=https://msedge.sf.dl.delivery.mp.microsoft.com/filestreamingservice/files/35871e19-d11b-432d-9d1e-7be0ff286540
get $WV2/MicrosoftEdgeWebView2RuntimeInstallerX64.exe \
  webview2-20261023-x64.exe 0625fb5c1e75be58a164e14f95aa58a3e5badb859b3d17c952385f3e4a9ff3e8
mkdir -p "$D/installer"
ln -f "$D/cache/webview2-20261023-x64.exe" "$D/installer/MicrosoftEdgeWebView2RuntimeInstallerX64.exe"

# Stage the runtime beside the executable (hard links: no copies of 700 MB).
# shellcheck source=scripts/compression-deps.sh
source "$ROOT/scripts/compression-deps.sh"
mkdir -p "$ROOT/bin"
cp -r -l -f "$RT/." "$ROOT/bin/"
echo "deps ready: $(du -sh "$RT" | cut -f1) runtime, staged into bin/"
