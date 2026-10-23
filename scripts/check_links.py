"""Check the repository's Markdown links: python scripts/check_links.py.

Every relative link in a tracked or new Markdown file must name a file that
exists, and a #fragment into a Markdown file must name one of its headings,
so moving or renaming a document cannot leave a reader at a dead end.
Links to websites are not fetched: the check makes no network request.
"""

import re
import shutil
import subprocess
import sys
from functools import cache
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parent.parent
LINK = re.compile(r"\]\(([^)\s]+)\)")
HEADING = re.compile(r"^#+\s+(.*)")
HTML_LINK = re.compile(r"<(?:img|a)\b[^>]*?\b(?:src|href)=[\"']([^\"']+)[\"']", re.IGNORECASE)
FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})(.*)$")


def prose(path: Path) -> tuple[list[tuple[int, str]], int]:
    """Return non-code lines and the opening line of any unclosed fence."""
    lines, fence, opened = [], "", 0
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        match = FENCE.match(line)
        if match:
            marker, rest = match.groups()
            if not fence:
                fence, opened = marker, number
            elif marker[0] == fence[0] and len(marker) >= len(fence) and not rest.strip():
                fence, opened = "", 0
            continue
        if not fence:
            lines.append((number, line))
    return lines, opened


@cache
def anchors(path: Path) -> frozenset[str]:
    """The fragments GitHub gives a document's headings."""
    slugs: set[str] = set()
    for _, line in prose(path)[0]:
        if m := HEADING.match(line):
            heading = re.sub(r"<[^>]+>", "", m.group(1)).strip().lower()
            base = re.sub(r"[^\w\- ]", "", heading).replace(" ", "-")
            slug, suffix = base, 0
            while slug in slugs:
                suffix += 1
                slug = f"{base}-{suffix}"
            slugs.add(slug)
    return frozenset(slugs)


def broken(document: Path) -> list[str]:
    """Each link in document that does not resolve, as 'line: target'."""
    found = []
    lines, unclosed = prose(document)
    if unclosed:
        found.append(f"{unclosed}: unclosed code fence")
    for number, line in lines:
        for target in LINK.findall(line) + HTML_LINK.findall(line):
            if re.match(r"^[a-z]+:", target, re.IGNORECASE) or target.startswith("//"):
                continue
            path, _, fragment = target.partition("#")
            resolved = (document.parent / unquote(path)).resolve() if path else document
            if not resolved.exists():
                found.append(f"{number}: missing {target}")
            elif fragment and resolved.suffix == ".md" and unquote(fragment).lower() not in anchors(resolved):
                found.append(f"{number}: no heading for {target}")
    return found


def main() -> None:
    git = shutil.which("git")
    if not git:
        raise SystemExit("check_links: git is not on PATH")
    # The git on PATH, with this script's own constant arguments: nothing untrusted.
    listed = subprocess.run(  # noqa: S603
        [git, "ls-files", "--cached", "--others", "--exclude-standard", "*.md"],
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=True,
    ).stdout.splitlines()
    failures = 0
    for name in listed:
        document = ROOT / name
        if not document.exists():  # deleted in the working tree, not yet staged
            continue
        for problem in broken(document):
            print(f"{name}:{problem}")
            failures += 1
    if failures:
        sys.exit(f"{failures} broken link(s)")


if __name__ == "__main__":
    main()
