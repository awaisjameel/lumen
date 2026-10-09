"""Collect the licences of OxiPNG's locked Rust dependencies and standard library."""

import json
import sys
from html.parser import HTMLParser
from pathlib import Path


class LicenceText(HTMLParser):
    """Keep the distribution's standard-library attribution in readable notices."""

    def __init__(self) -> None:
        super().__init__()
        self.parts: list[str] = []

    def handle_data(self, data: str) -> None:
        self.parts.append(data)


def main() -> None:
    metadata, rust, llvm, output = map(Path, sys.argv[1:])
    packages = json.loads(metadata.read_text(encoding="utf-8"))["packages"]
    parts = []
    for package in sorted(packages, key=lambda p: (p["name"], p["version"])):
        root = Path(package["manifest_path"]).parent
        texts = []
        for path in sorted(root.rglob("*")):
            if path.is_file() and path.name.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE")):
                texts.append(f"{path.relative_to(root)}\n{path.read_text(encoding='utf-8', errors='replace')}")
        if not texts:
            raise SystemExit(f"missing dependency licence: {package['name']}")
        parts.append(f"{package['name']} {package['version']}: {package['license']}\n" + "\n".join(texts))
    for name in ("LICENSE-APACHE", "LICENSE-MIT"):
        path = rust / name
        parts.append(f"Rust standard library: {name}\n{path.read_text(encoding='utf-8')}")
    copyright_text = LicenceText()
    copyright_text.feed((rust / "share" / "doc" / "rust" / "COPYRIGHT-library.html").read_text(encoding="utf-8"))
    parts.append("Rust standard-library attribution\n" + "".join(copyright_text.parts))
    for path in sorted((rust / "share" / "doc" / "rust" / "licenses").iterdir()):
        if path.is_file():
            parts.append(f"Rust distribution: {path.name}\n{path.read_text(encoding='utf-8')}")
    parts.append("LLVM runtime components\n" + llvm.read_text(encoding="utf-8"))
    output.write_bytes("\n\n".join(parts).encode("utf-8"))


if __name__ == "__main__":
    main()
