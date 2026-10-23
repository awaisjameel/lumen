"""Write THIRD_PARTY_NOTICES.txt: what Lumen ships that others made, and their licences.

    python scripts/notices.py OUT

It reads what the build really holds, so the notices can't drift from it:
- the native components and models pinned in scripts/deps.sh (versions read
  from there) and their licence texts, which deps.sh puts in deps/licenses;
- the Go modules linked into the production Lumen.exe (`go list -deps`);
- the JavaScript packages bundled into the UI (the licence list Vite writes to
  frontend/dist/.vite/license.json, so the UI must be built first);
- the icon collection the UI's icons are bundled from. Its icons reach the
  bundle as data, which Vite's list does not name, and its package carries no
  licence file: the text is kept in frontend/licenses.
"""

from __future__ import annotations

import json
import re
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DEPS_SH = ROOT / "scripts" / "deps.sh"
LICENSES = ROOT / "deps" / "licenses"
UI_LICENSES = ROOT / "frontend" / "dist" / ".vite" / "license.json"
ICON_PACKAGE = ROOT / "frontend" / "node_modules" / "@iconify-json" / "lucide" / "package.json"
ICON_LICENCE = ROOT / "frontend" / "licenses" / "lucide.LICENSE.txt"
LICENSE_NAMES = ("LICENSE", "LICENSE.md", "LICENSE.txt", "LICENCE", "COPYING", "LICENSE-MIT")
RULE = "=" * 78
MODULE_FORMAT = "{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}"


def go(*args: str) -> str:
    """The output of the go command, run in the repository."""
    exe = shutil.which("go")
    if not exe:
        raise SystemExit("notices: go is not on PATH")
    # The go on PATH, with this script's own constant arguments: nothing untrusted.
    return subprocess.run([exe, *args], cwd=ROOT, check=True, capture_output=True, text=True).stdout  # noqa: S603


def pinned(pattern: str, source: Path = DEPS_SH) -> str:
    """The version deps.sh pins, from the first match of pattern's group."""
    m = re.search(pattern, source.read_text(encoding="utf-8"))
    if not m:
        raise SystemExit(f"notices: {source.name} no longer matches {pattern!r}; update scripts/notices.py")
    return m.group(1)


def licence(name: str) -> str:
    """A licence text deps.sh put in deps/licenses."""
    path = LICENSES / name
    if not path.is_file():
        raise SystemExit(f"notices: {path} is missing; run scripts/deps.sh")
    return path.read_text(encoding="utf-8-sig").strip()


def go_modules() -> list[tuple[str, str, Path]]:
    """The modules linked into the production build: path, version, folder."""
    out = go("list", "-deps", "-tags", "production", "-f", MODULE_FORMAT, ".")
    mods = {tuple(line.split("\t")) for line in out.splitlines() if line}
    return sorted((p, v, Path(d)) for p, v, d in mods)


def module_licence(folder: Path) -> str:
    for name in LICENSE_NAMES:
        path = folder / name
        if path.is_file():
            return path.read_text(encoding="utf-8-sig").strip()
    raise SystemExit(f"notices: no licence file in {folder}")


def go_licence() -> tuple[str, str]:
    """Go's version and its licence (the standard library is in every binary)."""
    version, goroot = go("env", "GOVERSION", "GOROOT").splitlines()
    return version, (Path(goroot) / "LICENSE").read_text(encoding="utf-8").strip()


def ui_packages() -> list[dict[str, str]]:
    if not UI_LICENSES.is_file():
        raise SystemExit(f"notices: {UI_LICENSES} is missing; build the UI first")
    packages: list[dict[str, str]] = json.loads(UI_LICENSES.read_text(encoding="utf-8"))
    return sorted(packages, key=lambda p: p["name"])


def icon_set() -> tuple[str, str]:
    """The Lucide collection the UI's icons come from: its package version and its licence."""
    if not ICON_PACKAGE.is_file():
        raise SystemExit(f"notices: {ICON_PACKAGE} is missing; run npm ci in frontend/")
    if not ICON_LICENCE.is_file():
        raise SystemExit(f"notices: {ICON_LICENCE} is missing")
    version: str = json.loads(ICON_PACKAGE.read_text(encoding="utf-8"))["version"]
    return version, ICON_LICENCE.read_text(encoding="utf-8").strip()


def wrap(items: list[str], width: int = 88) -> str:
    """items as a comma-separated list, indented, broken between items only."""
    lines: list[str] = []
    line = " "
    for i, item in enumerate(items):
        word = item + ("." if i == len(items) - 1 else ",")
        if len(line) + 1 + len(word) > width:
            lines.append(line)
            line = " "
        line += " " + word
    return "\n".join([*lines, line])


def section(title: str, text: str) -> str:
    return f"{RULE}\n{title}\n{RULE}\n\n{text}\n"


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: python scripts/notices.py OUT")
    out = Path(sys.argv[1])

    vips = pinned(r"build-win64-mxe/releases/download/v([0-9.]+)/")
    ffmpeg = pinned(r"ffmpeg-n([0-9.]+)-[^ ]*-lgpl-shared")
    libraw = pinned(r"libraw\.org/data/LibRaw-([0-9.]+)\.tar\.gz")
    ort = pinned(r"microsoft\.ml\.onnxruntime\.directml/([0-9.]+)/")
    dml = pinned(r"microsoft\.ai\.directml/([0-9.]+)/")
    vcrt = pinned(r"(?m)^VCRT=([0-9.]+)$")
    compression_source = ROOT / "scripts" / "compression-deps.sh"
    jpeg = pinned(r"libjpeg-turbo/releases/download/([0-9.]+)/", compression_source)
    oxipng = pinned(r"crates/oxipng/oxipng-([0-9.]+)\.crate", ROOT / "scripts" / "oxipng-deps.sh")
    jxl = pinned(r"libjxl/releases/download/v([0-9.]+)/", compression_source)
    vips_libs: dict[str, str] = json.loads(licence("libvips.versions.json"))
    bundled = wrap([f"{name} {version}" for name, version in sorted(vips_libs.items()) if name != "vips"])
    go_version, go_text = go_licence()
    modules = go_modules()
    packages = ui_packages()
    icons_version, icons_text = icon_set()
    module_texts = {path: module_licence(folder) for path, _, folder in modules}
    # @wailsio/runtime is published without its licence file; it is Wails' own, MIT.
    wails_text = next(text for path, text in module_texts.items() if path.startswith("github.com/wailsapp/wails"))

    parts = [
        "Lumen: third-party notices\n\n"
        "Lumen is made with the open-source components and models below. Each is listed\n"
        "with its licence, and the licence texts follow the list. Lumen runs them on this\n"
        "PC only: nothing is sent anywhere.\n",
        section(
            "Components beside Lumen.exe",
            f'libvips {vips} (libvips-42.dll): LGPL-2.1-or-later. The unmodified static "web" build from\n'
            f"  https://github.com/libvips/build-win64-mxe/releases/tag/v{vips}, whose sources and\n"
            "  build scripts are published there. It contains these libraries, each under its own\n"
            f"  licence, set out in that build's repository:\n{bundled}\n\n"
            f'FFmpeg {ffmpeg} (ffmpeg\\): LGPL-3.0-or-later. The unmodified "lgpl-shared" build from\n'
            "  https://github.com/BtbN/FFmpeg-Builds, which publishes its build scripts; FFmpeg's\n"
            f"  source is at https://ffmpeg.org/ (release {ffmpeg}).\n\n"
            f"LibRaw {libraw} (inside lumen-raw.exe, which reads camera RAW files): used under the\n"
            "  Common Development and Distribution License 1.0, one of the two licences it offers.\n"
            f"  Built from the unmodified source, https://www.libraw.org/data/LibRaw-{libraw}.tar.gz,\n"
            "  by Lumen's scripts/deps.sh. Its copyright notice, which names the code it includes\n"
            "  (dcraw, DCB and FBDD, the X3F library, parts of the Adobe DNG SDK), follows below.\n\n"
            f"ONNX Runtime {ort} with DirectML (onnxruntime.dll, onnxruntime_providers_shared.dll): MIT.\n"
            "  https://github.com/microsoft/onnxruntime\n\n"
            f"DirectML {dml} (DirectML.dll): Microsoft Software License Terms, which allow it to be\n"
            "  distributed with applications. https://github.com/microsoft/DirectML\n\n"
            f"Microsoft Visual C++ runtime {vcrt} (msvcp140.dll, msvcp140_1.dll, vcruntime140.dll,\n"
            "  vcruntime140_1.dll), which ONNX Runtime needs: unmodified files from Microsoft's x64\n"
            "  Visual C++ Redistributable, distributed with applications under the Visual Studio\n"
            "  licence terms. https://aka.ms/VCRedistLicense\n\n"
            f"Compression tools (compress\\): libjpeg-turbo {jpeg} (BSD/IJG/zlib), OxiPNG {oxipng} (MIT),\n"
            f"  and libjxl {jxl} (BSD-3-Clause, with statically linked libraries' licences below).\n"
            "  Pinned and bundled by scripts/compression-deps.sh; no runtime downloads.\n"
            "  https://github.com/libjpeg-turbo/libjpeg-turbo\n"
            "  https://github.com/oxipng/oxipng\n"
            "  https://github.com/libjxl/libjxl\n"
            "  OxiPNG is built from its pinned source with Rust's GNU target; its dependency\n"
            "  notices and Rust's standard-library licences are included below.",
        ),
        section(
            "Models (models\\)",
            "SigLIP 2 base, patch 16, 224 px (Google): Apache-2.0. Exported to ONNX by onnx-community\n"
            "  (https://huggingface.co/onnx-community/siglip2-base-patch16-224-ONNX), then prepared\n"
            "  by Lumen's scripts/prepare_models.py (a fixed batch shape; the text tower's token\n"
            "  table stored as int8).\n\n"
            "YuNet face detector (OpenCV Zoo): MIT. https://huggingface.co/opencv/face_detection_yunet\n\n"
            "SFace face recogniser (OpenCV Zoo): Apache-2.0, given a variable batch size by\n"
            "  scripts/prepare_models.py. https://huggingface.co/opencv/face_recognition_sface\n\n"
            "PP-OCRv5 mobile text detector and English recogniser (PaddlePaddle): Apache-2.0.\n"
            "  https://huggingface.co/PaddlePaddle/PP-OCRv5_mobile_det_onnx and\n"
            "  https://huggingface.co/monkt/paddleocr-onnx",
        ),
        section(
            "Data inside Lumen.exe",
            "Place names: a reduced 30 September 2026 snapshot of GeoNames' cities5000, with\n"
            "  English region and country names. Credit: GeoNames contributors\n"
            "  (https://www.geonames.org/), licensed under CC BY 4.0\n"
            "  (https://creativecommons.org/licenses/by/4.0/). Lumen keeps each place's name,\n"
            "  position, region and country, and leaves out the other fields; see\n"
            "  docs/geonames.md in Lumen's source.",
        ),
        section(
            "Installed by Lumen's installer when Windows lacks it",
            "Microsoft Edge WebView2 Runtime, under Microsoft's own licence terms, which its\n"
            "  installer shows. https://developer.microsoft.com/microsoft-edge/webview2/",
        ),
        section(
            "Go code compiled into Lumen.exe",
            f"The Go standard library ({go_version}): BSD-3-Clause.\n"
            + "".join(f"{path} {version}\n" for path, version, _ in modules)
            + "github.com/mattn/go-sqlite3 includes SQLite, which is in the public domain.",
        ),
        section(
            "JavaScript in Lumen's window",
            "".join(f"{p['name']} {p['version']}: {p['identifier']}\n" for p in packages)
            + f"Icons: Lucide (@iconify-json/lucide {icons_version}): ISC; those derived from Feather: MIT.\n"
            "  https://lucide.dev/",
        ),
        "\nLICENCE TEXTS\n",
        section("libvips: GNU Lesser General Public License 2.1", licence("libvips.LICENSE.txt")),
        section("libjpeg-turbo: component licences", licence("jpeg-turbo.LICENSE.txt")),
        section("OxiPNG and its static dependencies: component licences", licence("oxipng.LICENSE.txt")),
        section("libjxl and its static dependencies: component licences", licence("jxl.LICENSE.txt")),
        section("FFmpeg: GNU Lesser General Public License 3", licence("ffmpeg.LICENSE.txt")),
        section("LibRaw: copyright notice", licence("libraw.COPYRIGHT.txt")),
        section("LibRaw: Common Development and Distribution License 1.0", licence("libraw.LICENSE.CDDL.txt")),
        section("ONNX Runtime: MIT", licence("onnxruntime.LICENSE.txt")),
        section("ONNX Runtime: third-party notices", licence("onnxruntime.ThirdPartyNotices.txt")),
        section("DirectML: Microsoft Software License Terms", licence("directml.LICENSE.txt")),
        section("DirectML: third-party notices", licence("directml.ThirdPartyNotices.txt")),
        section("YuNet: MIT", licence("yunet.LICENSE.txt")),
        section("SFace, SigLIP 2 and PP-OCRv5: Apache License 2.0", licence("sface.LICENSE.txt")),
        section("The Go standard library", go_text),
        *(section(f"{path} {version}", module_texts[path]) for path, version, _ in modules),
        *(
            section(
                f"{p['name']} {p['version']}",
                p.get("text") or (wails_text if p["name"] == "@wailsio/runtime" else p["identifier"]),
            )
            for p in packages
        ),
        section(f"Lucide icons (@iconify-json/lucide {icons_version})", icons_text),
    ]
    # Windows line ends: the file is opened in Notepad from Settings.
    out.write_text("\n".join(parts).replace("\r\n", "\n"), encoding="utf-8", newline="\r\n")
    print(f"notices: {out} ({len(modules)} Go modules, {len(packages)} UI packages)")


if __name__ == "__main__":
    main()
