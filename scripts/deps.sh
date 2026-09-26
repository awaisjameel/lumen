#!/usr/bin/env bash
# Fetches and prepares everything Lumen needs besides Go and Node: libvips,
# ONNX Runtime with DirectML, FFmpeg and the SigLIP 2 models. Every download is
# pinned and checked against its SHA-256. Run from Git Bash; safe to re-run
# (finished steps are skipped). Needs curl, unzip and Python 3 (model prep only).
#
#   deps/cache/     downloads
#   deps/vips/      libvips headers and import library (for cgo)
#   deps/runtime/   exactly what ships beside Lumen.exe (DLLs, ffmpeg/, models/)
#
# The last step copies deps/runtime into bin/ (hard links, so it's instant).
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$PWD
D=$ROOT/deps
RT=$D/runtime
mkdir -p "$D/cache" "$RT/ffmpeg" "$RT/models/siglip2" "$D/src/siglip2"

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

# FFmpeg 9.0 (LGPL, shared). Only ffmpeg.exe and the libraries it loads ship.
# BtbN prunes old autobuilds: if this 404s, pin a newer one and its hash.
get https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-09-26-13-03/ffmpeg-n9.0.2-10-g51c4a23d74-win64-lgpl-shared-9.0.zip \
  ffmpeg.zip 8f3a5190804eed8c0f4927dbac688bcc35a1d2b9c36d7bf9a4386ec7bd1627ed
if [ ! -f "$RT/ffmpeg/ffmpeg.exe" ]; then
  unzip -q -o -j "$D/cache/ffmpeg.zip" '*/bin/ffmpeg.exe' '*/bin/*.dll' '*/LICENSE.txt' -d "$RT/ffmpeg"
fi

# SigLIP 2 base (Apache-2.0): fp16 ONNX towers and the Gemma tokenizer.
HF=https://huggingface.co/onnx-community/siglip2-base-patch16-224-ONNX/resolve/ba1f3b0843f24bc5417d38e19c37b287d719b2f4
get $HF/tokenizer.json siglip2-tokenizer.json cb9140fae3ac5122c972d37adf83e1248471a38147ad76f8215c8872c6fd8322
get $HF/onnx/vision_model_fp16.onnx siglip2-vision-fp16.onnx a1959f7bd3993a607e48839f6d01e25b876fe76afda301b028b78eef68aabd95
get $HF/onnx/text_model_fp16.onnx siglip2-text-fp16.onnx 711da56ada0a4aa11c7dd3320df741081a3cae4f0ae1b5e5c6d5b294738d0eb0
ln -f "$D/cache/siglip2-tokenizer.json" "$D/src/siglip2/tokenizer.json"
ln -f "$D/cache/siglip2-vision-fp16.onnx" "$D/src/siglip2/vision_model_fp16.onnx"
ln -f "$D/cache/siglip2-text-fp16.onnx" "$D/src/siglip2/text_model_fp16.onnx"

M=$RT/models/siglip2
if [ ! -f "$M/siglip2.json" ] || [ "$ROOT/scripts/prepare_models.py" -nt "$M/siglip2.json" ]; then
  if [ ! -x "$D/venv/Scripts/python" ]; then
    python -m venv "$D/venv"
    "$D/venv/Scripts/python" -m pip install -q onnx==1.23.0 tokenizers==0.23.2 numpy==2.5.3
  fi
  PYTHONIOENCODING=utf-8 "$D/venv/Scripts/python" "$ROOT/scripts/prepare_models.py" "$D/src/siglip2" "$M"
fi

# Stage the runtime beside the executable (hard links: no copies of 700 MB).
mkdir -p "$ROOT/bin"
cp -r -l -f "$RT/." "$ROOT/bin/"
echo "deps ready: $(du -sh "$RT" | cut -f1) runtime, staged into bin/"
