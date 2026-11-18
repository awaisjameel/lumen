"""Decide which CI jobs a pull request's changed files need.

    git diff --name-only --no-renames BASE...HEAD | python scripts/ci_scope.py
    python scripts/ci_scope.py --all

Prints one `name=true|false` line per scope, the format of $GITHUB_OUTPUT:

- app: the Windows build, static checks, native suites and installer;
- go: the Go race tests;
- latency: the large-library latency report;
- docs: the documentation checks alone, for a change that touches nothing else
  (when app runs, scripts/lint.sh already checks the documentation).

A path is narrowed only by a rule below; anything else runs the build and the
Go tests, so a new kind of file costs time rather than coverage. Latency runs
only for what its benchmark compiles, a list the test checks against `go list`.
"""

from __future__ import annotations

import sys
from dataclasses import dataclass

# Documentation content: read by scripts/check-docs.sh and by nothing that is
# built or tested. The checkers themselves are code, linted by the build job.
DOCS_PREFIXES = ("docs/",)
DOCS_SUFFIXES = (".md",)

# Checked only by the build job (frontend, native suites, lint.sh's Python and
# shell checks), never by a Go test.
APP_ONLY_PREFIXES = ("frontend/", "e2e/", ".githooks/")
APP_ONLY_FILES = frozenset(
    {
        "ruff.toml",
        "scripts/check-commit-msg.sh",
        "scripts/check-staged.sh",
        "scripts/install-hooks.sh",
        "scripts/version.sh",
    }
)
APP_ONLY_SUFFIXES = (".py",)  # scripts/*.py and e2e fixtures; Go never runs them

# Frontend files Go reads or embeds: tests that compare the bridge's mirrors
# and shared fixtures, and main.go's embedded icon and navigation contract.
# scripts/ci_scope_test.py finds every such path in the Go sources and fails if
# one is missing here.
GO_READS_FRONTEND = (
    "frontend/src/bridge/index.ts",
    "frontend/src/shared/viewing.ts",
    "frontend/src/organise/template.parity.json",
    "frontend/src/shared/navigation/",
    "frontend/public/",
    "frontend/vite.config.ts",
    "frontend/package.json",
    "frontend/package-lock.json",
)

# What the latency benchmark compiles (the store's test binary and its Lumen
# imports, checked against `go list` by the test) and how it is run.
LATENCY_PREFIXES = (
    "internal/core/",
    "internal/fileops/",
    "internal/media/",
    "internal/places/",
    "internal/scanner/",
    "internal/store/",
    "scripts/benchgate/",
)
LATENCY_FILES = frozenset({"go.mod", "go.sum", "scripts/bench.sh", "scripts/env.sh", "scripts/deps.sh"})

# CI's own definition: a change here runs everything, to prove itself.
CI_PREFIXES = (".github/",)
CI_FILES = frozenset({"scripts/ci_scope.py", "scripts/ci_scope_test.py"})


@dataclass(frozen=True)
class Scope:
    app: bool = False
    go: bool = False
    latency: bool = False
    docs: bool = False

    def __or__(self, other: Scope) -> Scope:
        return Scope(
            app=self.app or other.app,
            go=self.go or other.go,
            latency=self.latency or other.latency,
            docs=self.docs or other.docs,
        )


EVERYTHING = Scope(app=True, go=True, latency=True)


def classify(path: str) -> Scope:
    """Return the jobs one changed path needs."""
    if path in CI_FILES or path.startswith(CI_PREFIXES):
        return EVERYTHING
    if path.startswith(DOCS_PREFIXES) or path.endswith(DOCS_SUFFIXES):
        return Scope(docs=True)
    if path.startswith(GO_READS_FRONTEND):
        return Scope(app=True, go=True)
    if path in APP_ONLY_FILES or path.startswith(APP_ONLY_PREFIXES) or path.endswith(APP_ONLY_SUFFIXES):
        return Scope(app=True)
    if path in LATENCY_FILES or path.startswith(LATENCY_PREFIXES):
        return EVERYTHING
    return Scope(app=True, go=True)


def scope(paths: list[str]) -> Scope:
    """Return the jobs a set of changed paths needs; docs only when nothing else runs."""
    result = Scope()
    for path in paths:
        result |= classify(path)
    if result.app:
        result = Scope(app=True, go=result.go, latency=result.latency)
    return result


def lines(result: Scope) -> list[str]:
    return [f"{name}={str(getattr(result, name)).lower()}" for name in ("app", "go", "latency", "docs")]


def main(argv: list[str]) -> int:
    if argv == ["--all"]:
        result = EVERYTHING
    elif not argv:
        result = scope([line.strip() for line in sys.stdin if line.strip()])
    else:
        print("usage: python scripts/ci_scope.py [--all] < changed-paths", file=sys.stderr)
        return 2
    print("\n".join(lines(result)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
