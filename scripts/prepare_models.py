"""Turn the downloaded SigLIP 2 files into what Lumen ships.

Run by scripts/deps.sh (which pins and verifies the inputs). Reads
deps/src/siglip2 and writes deps/models/siglip2:

  vision.onnx     fp16 image tower, input fixed to BATCH x 3 x 224 x 224 so
                  DirectML can compile the whole graph; only pooler_output kept
  text.onnx       fp16 text tower with its 256k x 768 token table stored as int8
                  (one scale per row, dequantised inside the graph); 565 -> ~370 MB
  tokenizer.bin   the Gemma BPE tokenizer in a compact form the Go loader reads
                  in milliseconds (format below, mirrored by internal/ai/tokenizer.go)
  siglip2.json    what the engine needs to know about the model

With a third argument it also writes reference encodings from Hugging Face
`tokenizers` there (internal/ai/testdata/tokenizer_fixtures.json).
"""

import json
import struct
import sys
import urllib.request
from pathlib import Path

import numpy as np
import onnx
from onnx import helper, numpy_helper, TensorProto
from onnx.utils import Extractor
from tokenizers import Tokenizer

BATCH = 16        # images per vision run (fixed shape; the engine pads the last batch)
IMAGE_SIZE = 224
MAX_TOKENS = 64
DIM = 768
# The logit scale and bias live only in the original checkpoint, not the ONNX
# towers; read them from its safetensors header with HTTP range requests.
CHECKPOINT = ("https://huggingface.co/google/siglip2-base-patch16-224/resolve/"
              "75de2d55ec2d0b4efc50b3e9ad70dba96a7b2fa2/model.safetensors")
VERSION = "siglip2-base-p16-224-fp16-1"  # bump when the prepared files change


def main(src: Path, dst: Path, fixtures: Path | None) -> None:
    dst.mkdir(parents=True, exist_ok=True)
    vision(src / "vision_model_fp16.onnx", dst / "vision.onnx")
    text(src / "text_model_fp16.onnx", dst / "text.onnx")
    tokenizer(src / "tokenizer.json", dst / "tokenizer.bin", fixtures)
    scale, bias = logit_params()
    (dst / "siglip2.json").write_text(json.dumps({
        "version": VERSION,
        "batch": BATCH,
        "imageSize": IMAGE_SIZE,
        "maxTokens": MAX_TOKENS,
        "dim": DIM,
        "logitScale": scale,
        "logitBias": bias,
    }, indent=2) + "\n")
    print("prepared", dst)


def keep_output(model: onnx.ModelProto, output: str) -> onnx.ModelProto:
    """Cut the graph down to what computes one output (drops last_hidden_state)."""
    inputs = [i.name for i in model.graph.input]
    return Extractor(model).extract_model(inputs, [output])


def vision(src: Path, dst: Path) -> None:
    m = keep_output(onnx.load(src), "pooler_output")
    for d, v in zip(m.graph.input[0].type.tensor_type.shape.dim, [BATCH, 3, IMAGE_SIZE, IMAGE_SIZE]):
        d.ClearField("dim_param")
        d.dim_value = v
    out = m.graph.output[0].type.tensor_type.shape.dim[0]
    out.ClearField("dim_param")
    out.dim_value = BATCH
    onnx.checker.check_model(m)
    onnx.save(m, dst)
    print(f"vision: {dst.stat().st_size >> 20} MB")


def text(src: Path, dst: Path) -> None:
    m = keep_output(onnx.load(src), "pooler_output")
    g = m.graph
    table = next(t for t in g.initializer if t.name == "text_model.embeddings.token_embedding.weight")
    users = [n for n in g.node if table.name in n.input]
    if len(users) != 1 or users[0].op_type != "Gather":
        sys.exit("text model: expected one Gather on the token table")
    gather = users[0]

    # Symmetric int8, one scale per token row: row = q * scale.
    w = numpy_helper.to_array(table).astype(np.float32)
    scale = np.maximum(np.abs(w).max(axis=1, keepdims=True), 1e-8) / 127.0
    q = np.clip(np.rint(w / scale), -127, 127).astype(np.int8)
    err = np.abs(q.astype(np.float32) * scale - w).max()

    g.initializer.remove(table)
    g.initializer.extend([
        numpy_helper.from_array(q, "token_table_q"),
        numpy_helper.from_array(scale.astype(np.float16), "token_table_scale"),
    ])
    ids = gather.input[1]
    out = gather.output[0]
    idx = list(g.node).index(gather)
    g.node.remove(gather)
    new = [
        helper.make_node("Gather", ["token_table_q", ids], ["token_rows_q"], name="token_gather_q"),
        helper.make_node("Cast", ["token_rows_q"], ["token_rows"], to=TensorProto.FLOAT16, name="token_dequant_cast"),
        helper.make_node("Gather", ["token_table_scale", ids], ["token_row_scale"], name="token_gather_scale"),
        helper.make_node("Mul", ["token_rows", "token_row_scale"], [out], name="token_dequant_mul"),
    ]
    for i, n in enumerate(new):
        g.node.insert(idx + i, n)
    onnx.checker.check_model(m)
    onnx.save(m, dst)
    print(f"text: {dst.stat().st_size >> 20} MB (token table int8, max abs error {err:.2e})")


def tokenizer(src: Path, dst: Path, fixtures: Path | None) -> None:
    """tokenizer.bin, little-endian:

    "LTOK" u32 version=1
    u32 n, then n x (u32 rune, u32 id)       single-character tokens
    256 x u32                                 byte-fallback ids <0x00>..<0xFF>
    u32 n, then n x (u32 a, u32 b, u32 ab)   merges in rank order
    """
    raw = json.loads(src.read_text(encoding="utf-8"))
    model = raw["model"]
    if model["type"] != "BPE" or not model["byte_fallback"]:
        sys.exit("tokenizer: expected byte-fallback BPE")
    vocab: dict[str, int] = model["vocab"]
    chars = sorted((ord(t), i) for t, i in vocab.items() if len(t) == 1)
    # A byte without its own token (only \t, which is a character token
    # anyway) falls back to <unk>, as in Hugging Face's BPE.
    byte_ids = [vocab.get(f"<0x{b:02X}>", vocab[model["unk_token"]]) for b in range(256)]
    merges = []
    for a, b in model["merges"]:
        if (a + b) not in vocab:
            sys.exit(f"tokenizer: merge {a!r}+{b!r} has no token")
        merges.append((vocab[a], vocab[b], vocab[a + b]))

    with dst.open("wb") as f:
        f.write(b"LTOK" + struct.pack("<I", 1))
        f.write(struct.pack("<I", len(chars)))
        f.write(b"".join(struct.pack("<II", c, i) for c, i in chars))
        f.write(struct.pack("<256I", *byte_ids))
        f.write(struct.pack("<I", len(merges)))
        f.write(np.asarray(merges, dtype="<u4").tobytes())
    print(f"tokenizer: {len(chars)} characters, {len(merges)} merges, {dst.stat().st_size >> 10} KB")
    if fixtures is None:
        return

    # Reference encodings. The engine lower-cases and trims before encoding,
    # and truncates to MAX_TOKENS - 1 before appending <eos>, like SigLIP's processor.
    tok = Tokenizer.from_file(str(src))
    tok.enable_truncation(MAX_TOKENS)
    samples = [
        "a photo of a dog", "Beach sunset", "people in an office", "screenshot of code",
        "café résumé 😀 naïve", "Ali at the beach", "  two  spaces ", "Birthday cake with candles",
        "receipt", "a handwritten note", "東京タワー", "مسجد", "snow-covered mountains at dawn",
        "WhatsApp image", "3 people", "a very long query " * 20,
    ]
    lines = []
    for s in samples:
        ids = tok.encode(s.strip().lower()).ids
        ids = ids[: ids.index(1) + 1]  # up to <eos>; the rest is padding
        lines.append(json.dumps({"text": s, "ids": ids}, ensure_ascii=False))
    fixtures.write_text("[\n" + ",\n".join(lines) + "\n]\n", encoding="utf-8")


def logit_params() -> tuple[float, float]:
    def fetch(a: int, b: int) -> bytes:
        req = urllib.request.Request(CHECKPOINT, headers={"Range": f"bytes={a}-{b}"})
        with urllib.request.urlopen(req) as r:
            return r.read()

    n = struct.unpack("<Q", fetch(0, 7))[0]
    header = json.loads(fetch(8, 8 + n - 1))
    vals = []
    for key in ("logit_scale", "logit_bias"):
        start, end = header[key]["data_offsets"]
        vals.append(struct.unpack("<f", fetch(8 + n + start, 8 + n + end - 1))[0])
    # logit_scale is stored as a log; the model multiplies by exp(logit_scale).
    return float(np.exp(vals[0])), vals[1]


if __name__ == "__main__":
    main(Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3]) if len(sys.argv) > 3 else None)
