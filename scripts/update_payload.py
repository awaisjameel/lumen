"""Prepare versioned runtime packaging and TUF target input without publishing."""

from __future__ import annotations

import argparse
import hashlib
import json
import msvcrt
import re
import shutil
import zipfile
from pathlib import Path


def digest(path: Path) -> str:
    """Hash model files without buffering them in memory."""
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def prepare(root: Path, version: str, channel: str, schema: int) -> None:
    """Build new isolated packaging inputs, preserving every source file."""
    output = root / "bin" / "update-package"
    if output.exists():
        # This exact build directory is the only cleanup target. Refuse links
        # and unknown files instead of recursively deleting an install folder.
        expected_root = root.resolve() / "bin" / "update-package"
        if output.resolve() != expected_root or output.is_symlink() or output.is_junction():
            raise ValueError(f"unsafe packaging output: {output}")
        active = json.loads((output / ".lumen-active.json").read_bytes())
        old = output / "runtime" / active["generation"]
        known = {old / f["path"] for f in active["release"]["files"]}
        known.update({output / ".lumen-active.json", output / "lumen-update.exe"})
        if not old.resolve().is_relative_to(expected_root / "runtime") or any(
            not file.resolve().is_relative_to(expected_root) for file in known
        ):
            raise ValueError("packaging cleanup paths leave the owned build directory")
        entries = list(output.rglob("*"))
        if any(p.is_symlink() or p.is_junction() or (p.is_file() and p not in known) for p in entries):
            raise ValueError("packaging output contains unsafe or unrelated files")
        for file in known:
            file.unlink(missing_ok=True)
        for folder in sorted((p for p in entries if p.is_dir()), key=lambda p: len(p.parts), reverse=True):
            folder.rmdir()
        output.rmdir()
    generation = output / "runtime" / version
    generation.mkdir(parents=True)
    runtime = root / "deps" / "runtime"
    for source in sorted(runtime.rglob("*")):
        if source.is_symlink() or source.is_junction():
            raise ValueError(f"unsafe runtime link: {source}")
        if source.is_file():
            target = generation / source.relative_to(runtime)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, target)
    shutil.copyfile(root / "bin" / "Lumen.exe", generation / "lumen-app.exe")
    shutil.copyfile(root / "bin" / "THIRD_PARTY_NOTICES.txt", generation / "THIRD_PARTY_NOTICES.txt")
    files = [
        {"path": file.relative_to(generation).as_posix(), "size": file.stat().st_size, "sha256": digest(file)}
        for file in sorted(generation.rglob("*"))
        if file.is_file()
    ]
    release = {
        "artifactURL": f"https://github.com/awaisjameel/lumen/releases/download/v{version}/Lumen-{version}-windows-amd64-runtime.zip",
        "product": "io.lumen.app",
        "channel": channel,
        "platform": "windows",
        "arch": "amd64",
        "version": version,
        "minUpdater": 1,
        "minWindows": 10240,
        "readMin": schema,
        "readMax": schema,
        "writeSchema": schema,
        "notes": "",
        "files": files,
    }
    active = {"format": 1, "generation": version, "version": version, "release": release, "pending": ""}
    (output / ".lumen-active.json").write_bytes(json.dumps(active, separators=(",", ":")).encode())
    shutil.copyfile(root / "bin" / "lumen-update.exe", output / "lumen-update.exe")
    target = root / "bin" / f"Lumen-{version}-windows-amd64-runtime.zip"
    with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
        for file in files:
            archive.write(generation / file["path"], file["path"])
    custom = {
        "target": f"io.lumen.app/{channel}/windows-amd64/{version}/runtime.zip",
        "length": target.stat().st_size,
        "sha256": digest(target),
        "custom": release,
    }
    (root / "bin" / "update-target.json").write_bytes(json.dumps(custom, indent=2).encode() + b"\n")


def main() -> None:
    """Prepare runtime bytes for a reviewed version and schema."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version")
    parser.add_argument("--channel", choices=("stable", "preview"), default="stable")
    parser.add_argument("--schema", type=int)
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    if args.version is None:
        match = re.search(r'(?m)^  version: "([^"]+)"', (root / "build" / "config.yml").read_text(encoding="utf-8"))
        if not match:
            parser.error("build/config.yml has no application version")
        args.version = match.group(1)
    if args.schema is None:
        args.schema = max(int(p.name.split("_", 1)[0]) for p in (root / "internal/store/migrations").glob("*.sql"))
    parts = args.version.split(".")
    if len(parts) != 3 or any(not p.isdigit() or str(int(p)) != p for p in parts):
        parser.error("version must be three canonical non-negative integers")
    with (root / "bin" / ".lumen-package.lock").open("a+b") as lock:
        if lock.tell() == 0:
            lock.write(b"0")
            lock.flush()
        lock.seek(0)
        try:
            msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
        except OSError:
            parser.error("another runtime packaging process is still running")
        prepare(root, args.version, args.channel, args.schema)


if __name__ == "__main__":
    main()
