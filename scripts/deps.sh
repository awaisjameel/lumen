#!/usr/bin/env bash
# Fetches and prepares everything Lumen needs besides Go and Node: libvips,
# ONNX Runtime with DirectML, FFmpeg, the SigLIP 2 and face models, and the
# linters. Every download is pinned and checked against its SHA-256 (the two
# values model prep reads from the SigLIP checkpoint are pinned in
# prepare_models.py; its Python packages are pinned by version). Run from Git
# Bash; safe to re-run (finished steps are skipped). Needs curl, unzip, Go and
# Python 3.14 (model prep only).
#
#   deps/cache/     downloads
#   deps/vips/      libvips headers and import library (for cgo)
#   deps/runtime/   exactly what ships beside Lumen.exe (DLLs, ffmpeg/, models/)
#   deps/tools/     development tools: golangci-lint, deadcode, govulncheck, ruff, shellcheck
#
# The last step copies deps/runtime into bin/ (hard links, so it's instant).
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$PWD
D=$ROOT/deps
RT=$D/runtime
mkdir -p "$D/cache" "$RT/ffmpeg" "$D/src/siglip2" "$D/src/faces"

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

# libvips 8.18.6, "web" formats (JPEG, PNG, WebP, GIF, TIFF, AVIF, SVG),
# statically linked into one DLL. It has no HEVC decoder: HEIC goes through WIC or FFmpeg.
get https://github.com/libvips/build-win64-mxe/releases/download/v8.18.6/vips-dev-x64-web-8.18.6-static.zip \
  vips.zip 42ceea0c2f53d1244f3cb54c059881487b37452ee46c1c98c0d4b47480b20588
if [ ! -f "$D/vips/lib/libvips.lib" ]; then
  rm -rf "$D/vips" "$D/vips-dev-8.18"
  unzip -q "$D/cache/vips.zip" -d "$D" && mv "$D/vips-dev-8.18" "$D/vips"
fi
cp -u "$D/vips/bin/libvips-42.dll" "$RT/"

# ONNX Runtime 1.24.4, the last release with a DirectML build (onnxruntime_go
# v1.27.0 matches it), and DirectML 1.15.4.
get https://api.nuget.org/v3-flatcontainer/microsoft.ml.onnxruntime.directml/1.24.4/microsoft.ml.onnxruntime.directml.1.24.4.nupkg \
  ort-dml.nupkg 57e9f11b73437bef7a309496135d4c1f96b1a8e9ddba60013fa27bfc1d788681
get https://api.nuget.org/v3-flatcontainer/microsoft.ai.directml/1.15.4/microsoft.ai.directml.1.15.4.nupkg \
  directml.nupkg 4e7cb7ddce8cf837a7a75dc029209b520ca0101470fcdf275c1f49736a3615b9
[ -f "$RT/onnxruntime.dll" ] || unzip -q -o -j "$D/cache/ort-dml.nupkg" 'runtimes/win-x64/native/*.dll' -d "$RT"
[ -f "$RT/DirectML.dll" ] || unzip -q -o -j "$D/cache/directml.nupkg" 'bin/x64-win/DirectML.dll' -d "$RT"

# FFmpeg 9.0 (LGPL, shared). Only ffmpeg.exe, ffprobe.exe and the libraries
# they load ship. BtbN prunes old autobuilds: if this 404s, pin a newer one
# and its hash.
get https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-09-26-13-03/ffmpeg-n9.0.2-10-g51c4a23d74-win64-lgpl-shared-9.0.zip \
  ffmpeg.zip 8f3a5190804eed8c0f4927dbac688bcc35a1d2b9c36d7bf9a4386ec7bd1627ed
if [ ! -f "$RT/ffmpeg/ffmpeg.exe" ] || [ ! -f "$RT/ffmpeg/ffprobe.exe" ]; then
  unzip -q -o -j "$D/cache/ffmpeg.zip" '*/bin/ffmpeg.exe' '*/bin/ffprobe.exe' '*/bin/*.dll' '*/LICENSE.txt' -d "$RT/ffmpeg"
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

# prepare MODEL: scripts/prepare_models.py turns deps/src/MODEL into
# deps/runtime/models/MODEL, again whenever the script changes.
prepare() {
  local out=$RT/models/$1
  [ -f "$out/$1.json" ] && [ ! "$ROOT/scripts/prepare_models.py" -nt "$out/$1.json" ] && return
  if [ ! -x "$D/venv/Scripts/python" ]; then
    python -m venv "$D/venv"
    "$D/venv/Scripts/python" -m pip install -q onnx==1.23.0 tokenizers==0.23.2 numpy==2.5.3
  fi
  PYTHONIOENCODING=utf-8 "$D/venv/Scripts/python" "$ROOT/scripts/prepare_models.py" "$1" "$D/src/$1" "$out"
}
prepare siglip2
prepare faces

# Development tools (never shipped): the linters scripts/lint.sh runs, into
# deps/tools. golangci-lint is built with this machine's Go (go install
# checks it against the Go checksum database), so it understands the Go
# version the project uses.
T=$D/tools
mkdir -p "$T"
get https://github.com/astral-sh/ruff/releases/download/0.16.8/ruff-x86_64-pc-windows-msvc.zip \
  ruff-0.16.8.zip 7985e06caf14b0077cb68da41edbf812e1d9654fe4c144d72b71138e96350129
"$T/ruff.exe" --version 2>/dev/null | grep -q '^ruff 0.16.8$' || unzip -q -o -j "$D/cache/ruff-0.16.8.zip" ruff.exe -d "$T"
get https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.zip \
  shellcheck-v0.11.0.zip 8a4e35ab0b331c85d73567b12f2a444df187f483e5079ceffa6bda1faa2e740e
"$T/shellcheck.exe" --version 2>/dev/null | grep -q '^version: 0.11.0$' ||
  unzip -q -o -j "$D/cache/shellcheck-v0.11.0.zip" shellcheck.exe -d "$T"
# goinstall BIN MODULE@VERSION: build a Go tool into deps/tools unless that
# version is already there.
goinstall() {
  local bin=$T/$1.exe pkg=$2 want=${2##*@}
  [ -f "$bin" ] && go version -m "$bin" | grep -q "^[[:space:]]mod[[:space:]].*[[:space:]]${want}[[:space:]]" && return
  echo "build $1 $want"
  GOBIN=$T CGO_ENABLED=0 go install "$pkg"
}
goinstall golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
goinstall deadcode golang.org/x/tools/cmd/deadcode@v0.50.0
goinstall govulncheck golang.org/x/vuln/cmd/govulncheck@v1.8.0

# Stage the runtime beside the executable (hard links: no copies of 700 MB).
mkdir -p "$ROOT/bin"
cp -r -l -f "$RT/." "$ROOT/bin/"
echo "deps ready: $(du -sh "$RT" | cut -f1) runtime, staged into bin/"
