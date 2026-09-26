#!/usr/bin/env bash
# Downloads everything the Phase 0 spikes need into spikes/.cache (git-ignored).
# Run from Git Bash: ./fetch.sh   (about 2 GB; safe to re-run, skips what exists)
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p .cache/lib .cache/models/{siglip2,faces,ocr} .cache/heic .cache/sqlite-include
cd .cache
get() { [ -s "$2" ] || { echo "fetch $2"; curl -sfL --retry 3 -o "$2.part" "$1" && mv "$2.part" "$2"; }; }

# ONNX Runtime 1.24.4 with DirectML (the last DirectML build) + DirectML 1.15.4
get https://api.nuget.org/v3-flatcontainer/microsoft.ml.onnxruntime.directml/1.24.4/microsoft.ml.onnxruntime.directml.1.24.4.nupkg ort-dml.nupkg
get https://api.nuget.org/v3-flatcontainer/microsoft.ai.directml/1.15.4/microsoft.ai.directml.1.15.4.nupkg directml.nupkg
[ -f lib/onnxruntime.dll ] || { unzip -o -q -j ort-dml.nupkg 'runtimes/win-x64/native/*.dll' -d lib; unzip -o -q -j directml.nupkg 'bin/x64-win/DirectML.dll' -d lib; }

# libvips 8.18.6 (Windows build: HEIC header only, no HEVC decoder)
get https://github.com/libvips/build-win64-mxe/releases/download/v8.18.6/vips-dev-x64-all-8.18.6.zip vips.zip
[ -d vips-dev-8.18 ] || unzip -q vips.zip

# sqlite-vec's cgo binding includes "sqlite3.h"; mattn/go-sqlite3 names it sqlite3-binding.h
echo '#include "sqlite3-binding.h"' > sqlite-include/sqlite3.h

H=https://huggingface.co
S=$H/onnx-community/siglip2-base-patch16-224-ONNX/resolve/main
for f in tokenizer.json config.json preprocessor_config.json tokenizer_config.json special_tokens_map.json; do get $S/$f models/siglip2/$f; done
for f in vision_model.onnx vision_model_fp16.onnx vision_model_int8.onnx text_model_fp16.onnx text_model_int8.onnx; do get $S/onnx/$f models/siglip2/$f; done

get $H/opencv/face_detection_yunet/resolve/main/face_detection_yunet_2023mar.onnx models/faces/face_detection_yunet_2023mar.onnx
get $H/opencv/face_detection_yunet/resolve/main/LICENSE models/faces/yunet.LICENSE
get $H/opencv/face_recognition_sface/resolve/main/face_recognition_sface_2021dec.onnx models/faces/face_recognition_sface_2021dec.onnx
get $H/opencv/face_recognition_sface/resolve/main/LICENSE models/faces/sface.LICENSE

get $H/PaddlePaddle/PP-OCRv5_mobile_det_onnx/resolve/main/inference.onnx models/ocr/mobile-det.onnx
get $H/PaddlePaddle/PP-OCRv5_mobile_det_onnx/resolve/main/inference.yml models/ocr/mobile-det.yml
get $H/monkt/paddleocr-onnx/resolve/main/detection/v5/det.onnx models/ocr/det.onnx
for f in rec.onnx dict.txt config.json; do get $H/monkt/paddleocr-onnx/resolve/main/languages/english/$f models/ocr/en-$f; done

for f in autumn_1440x960 old_bridge_1440x960 winter_1440x960 random_collection_1440x960 season_collection_1440x960; do
  get https://nokiatech.github.io/heif/content/images/$f.heic heic/$f.heic
done
get https://raw.githubusercontent.com/strukturag/libheif/master/examples/example.heic heic/libheif-example.heic
get https://raw.githubusercontent.com/tigranbs/test-heic-images/master/image1.heic heic/iphone.heic

# Fixed-shape (batch 16) copies of the vision models: DirectML only fuses
# the graph when every dimension is known. Needs Python for the onnx package.
if [ ! -f models/siglip2/vision_model_fp16_b16.onnx ]; then
  [ -d venv ] || python -m venv venv
  venv/Scripts/python -m pip install -q onnx tokenizers
  (cd models/siglip2 && ../../venv/Scripts/python - <<'EOF'
import onnx
for src in ['vision_model.onnx', 'vision_model_fp16.onnx', 'vision_model_int8.onnx']:
    m = onnx.load(src)
    for d, v in zip(m.graph.input[0].type.tensor_type.shape.dim, [16, 3, 224, 224]):
        d.ClearField('dim_param'); d.dim_value = v
    for o in m.graph.output:
        o.type.tensor_type.shape.dim[0].ClearField('dim_param'); o.type.tensor_type.shape.dim[0].dim_value = 16
    onnx.save(m, src.replace('.onnx', '_b16.onnx'))
EOF
  )
fi
echo "done: $(du -sh . | cut -f1) in spikes/.cache"
