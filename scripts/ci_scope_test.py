"""Regressions for the CI change scope, and for its rules drifting from the code.

python scripts/ci_scope_test.py
"""

from __future__ import annotations

import re
import shutil
import subprocess
import unittest
from pathlib import Path

from ci_scope import EVERYTHING, LATENCY_FILES, LATENCY_PREFIXES, Scope, classify, lines, scope

ROOT = Path(__file__).resolve().parent.parent
JOIN = re.compile(r"filepath\.Join\(([^)]*)\)")
LITERAL = re.compile(r'"([^"]*)"')
EMBED = re.compile(r"^//go:embed\s+(.+)$", re.MULTILINE)
GIT = shutil.which("git") or "git"
GO = shutil.which("go")
# What scripts/bench.sh builds: the store's benchmark and the budget checker.
BENCHMARK_PACKAGES = ("./internal/store", "./scripts/benchgate")


def go_sources() -> list[Path]:
    """The tracked Go files CI builds and tests (spikes/ is its own module)."""
    # The git on PATH, with this script's own constant arguments: nothing untrusted.
    listed = subprocess.run(  # noqa: S603
        [GIT, "ls-files", "*.go", ":!spikes"], cwd=ROOT, capture_output=True, text=True, check=True
    )
    return [ROOT / name for name in listed.stdout.split()]


def frontend_paths_read_by_go() -> set[str]:
    """Every frontend path a Go file reads, embeds or names, relative to the repository."""
    found: set[str] = set()
    for source in go_sources():
        text = source.read_text(encoding="utf-8")
        for call in JOIN.findall(text):
            parts = [part for part in LITERAL.findall(call) if part != ".."]
            if parts[:1] == ["frontend"]:
                found.add("/".join(parts))
        found.update(literal for literal in LITERAL.findall(text) if literal.startswith("frontend/"))
        for patterns in EMBED.findall(text):
            found.update(p.removeprefix("all:") for p in patterns.split() if "frontend/" in p)
    return found


class ChangeScope(unittest.TestCase):
    def test_documentation_alone_runs_only_the_documentation_checks(self) -> None:
        paths = ["docs/release-status.md", "README.md", "docs/screenshots/capture.json"]
        self.assertEqual(scope(paths), Scope(docs=True))

    def test_documentation_with_code_is_left_to_the_build_jobs_lint(self) -> None:
        self.assertEqual(scope(["README.md", "frontend/src/App.vue"]), Scope(app=True))

    def test_frontend_and_e2e_changes_skip_the_go_tests_and_latency(self) -> None:
        paths = ["frontend/src/albums/AlbumsView.vue", "e2e/suites/commands.mjs", "e2e/fixtures/tagging.py"]
        self.assertEqual(scope(paths), Scope(app=True))

    def test_frontend_files_go_reads_run_the_go_tests(self) -> None:
        for path in ("frontend/src/bridge/index.ts", "frontend/src/shared/navigation/contract.ts"):
            with self.subTest(path=path):
                self.assertEqual(classify(path), Scope(app=True, go=True))

    def test_go_outside_the_benchmark_skips_latency(self) -> None:
        self.assertEqual(scope(["main.go", "services/library.go", "internal/ai/ai.go"]), Scope(app=True, go=True))

    def test_the_store_and_its_imports_run_the_latency_report(self) -> None:
        for path in ("internal/store/albums.go", "internal/core/types.go", "scripts/bench.sh"):
            with self.subTest(path=path):
                self.assertEqual(classify(path), Scope(app=True, go=True, latency=True))

    def test_dependency_manifests_run_the_vulnerability_checks(self) -> None:
        self.assertEqual(classify("go.sum"), EVERYTHING)
        self.assertEqual(classify("frontend/package-lock.json"), Scope(app=True, go=True, vuln=True))
        self.assertEqual(classify("e2e/package-lock.json"), Scope(app=True, vuln=True))

    def test_other_changes_leave_the_live_advisory_database_alone(self) -> None:
        self.assertFalse(scope(["internal/store/albums.go", "frontend/src/App.vue", "main.go"]).vuln)

    def test_a_ci_change_runs_everything(self) -> None:
        for path in (".github/workflows/ci.yml", ".github/actions/setup/action.yml", "scripts/lint.sh"):
            with self.subTest(path=path):
                self.assertEqual(classify(path), EVERYTHING)

    def test_an_unrecognised_path_runs_the_build_and_go_tests(self) -> None:
        self.assertEqual(classify("build/windows/info.json"), Scope(app=True, go=True))
        self.assertEqual(classify("LICENSE"), Scope(app=True, go=True))

    def test_no_change_runs_nothing(self) -> None:
        self.assertEqual(scope([]), Scope())

    def test_output_is_one_line_per_scope(self) -> None:
        self.assertEqual(lines(Scope(docs=True)), ["app=false", "go=false", "latency=false", "vuln=false", "docs=true"])


class RulesMatchTheCode(unittest.TestCase):
    def test_every_frontend_path_go_reads_runs_the_go_tests(self) -> None:
        found = frontend_paths_read_by_go()
        self.assertIn("frontend/src/bridge/index.ts", found)  # the scan itself works
        # frontend/dist is the build output: Go tests need it only to exist,
        # apart from navigation.json, whose source is the navigation rule.
        for path in sorted(found - {"frontend/dist", "frontend/dist/navigation.json"}):
            with self.subTest(path=path):
                self.assertTrue(classify(path).go, f"{path} is read by Go but would not run the Go tests")
        self.assertTrue(classify("frontend/src/shared/navigation/contract.ts").go)

    @unittest.skipUnless(GO, "needs the Go toolchain to list the benchmark's packages")
    def test_latency_covers_every_package_the_benchmark_compiles(self) -> None:
        # The go on PATH (the skip above guarantees one), with constant arguments.
        listed = subprocess.run(  # noqa: S603
            [str(GO), "list", "-deps", "-test", "-f", "{{.ImportPath}}|{{.Dir}}", *BENCHMARK_PACKAGES],
            cwd=ROOT,
            capture_output=True,
            text=True,
            check=True,
        )
        for line in listed.stdout.splitlines():
            name, folder = line.split("|", 1)
            if not name.startswith("lumen"):
                continue
            relative = Path(folder).resolve().relative_to(ROOT).as_posix() + "/"
            with self.subTest(package=name):
                self.assertTrue(relative.startswith(LATENCY_PREFIXES), f"{relative} is not a latency prefix")
        self.assertIn("internal/store/", LATENCY_PREFIXES)
        self.assertIn("go.mod", LATENCY_FILES)


if __name__ == "__main__":
    unittest.main()
