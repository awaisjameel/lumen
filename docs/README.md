# Documentation map and content inventory

The README is the entry point. Each topic below has one canonical explanation;
short introductions and task-specific reminders link to it. This inventory was
reviewed against source `820ebed` and the current build on 24 October 2026 for
[issue #29](https://github.com/awaisjameel/lumen/issues/29).

## Living documents

| Document | Canonical topic | Audit disposition and implementation evidence |
| --- | --- | --- |
| [README](../README.md) | Product introduction, installation, first run, screenshots, privacy summary, status, known limits and help | Rewritten as a user's path; contributor detail moved to Development, Architecture and Releasing, with one section of links in its place. Release claims defer to release status. |
| [Using Lumen](using-lumen.md) | Step-by-step examples and expected outcomes | New: folders, indexing, semantic/person/OCR search, face/tag corrections, albums, blocks, duplicates, file tools, compression and settings. Every control it names was checked against `frontend/src/shared/i18n.ts` and the views that use it. |
| [Features](features.md) | Complete capability reference | Kept its numbered sections and anchors; corrected label-only hiding, hash sampling, the indexing order, search latency, how the activity card opens and problem-report privacy. Procedures link to Using Lumen. |
| [Beta testing](beta-testing.md) | Tester checklist, safe file effects, reporting and uninstall | Replaced the absolute "never connects" with the opt-in updater exception, added checksum steps and links to release status and the walkthrough, and corrected how the activity card opens. Memory is a recommendation, not a tested hardware matrix. |
| [Development](development.md) | Supported Windows toolchains, setup, commands, CI and build troubleshooting | Moved contributor detail out of README. Root-relative npm examples fix directory leakage. Explicit cgo, shell environment, engine checks and e2e requirements match manifests, Taskfiles and setup action. |
| [CONVENTIONS](../CONVENTIONS.md) | Coding standards and contract ownership | Reviewed against Go/store/bridge/ESLint and test helpers; retained authoritative rules. Command recipes link to Development. |
| [AGENTS](../AGENTS.md) and [CLAUDE](../CLAUDE.md) | Working instructions for coding agents, and the import shim | Keeps its command quick reference, since agents act on it directly, and points to Development for setup and explanations; architecture guardrails and scratch-library safety unchanged. CLAUDE only imports AGENTS. |
| [Architecture](architecture.md) | Repository layout, engine overview and deliberate plan deviations | Moved from README, preserving decisions. Face tuning measurements are labelled historical rather than a general accuracy promise. |
| [Release status](release-status.md) | Published assets, source candidate, accepted evidence and remaining gates | New dated check against GitHub release metadata, `build/config.yml`, empty trust config and dependency/installer records. Published v0.12.0 is unsigned and not marked pre-release; its bytes predate current source. |
| [Releasing](releasing.md) | Version branch, tag, draft and owner publication | Moved from README; matches `scripts/version.sh` and `release.yml`. Explicitly distinguishes publishing from signing/installed acceptance. |
| [Production signing](production-signing.md) | Trust custody, signed packages/feed, activation and recovery gates | Retained specialised operational instructions; clarified that configured trust, not simply a version or GitHub flag, controls delivery. PowerShell examples are explicitly labelled. |
| [Screenshots](screenshots/README.md) | Gallery, captions, media provenance and the capture workflow | Keeps the seven 21 October captures, now with recorded provenance: every photo in them is one of 992 hash-pinned Unsplash photos. Adds a face-review image captured by a reproducible script on the same photos and one public-domain portrait. `capture.json` pins all eight images. |
| [Branding](branding.md) | Mark ownership, source artwork and regeneration | Kept instructions; moved the dated verification results to history and linked them. |
| [GeoNames](geonames.md) | Bundled data provenance and rebuild | Retained snapshot, attribution, hash-pinned builder and approximate 75 km semantics, checked against `scripts/build_places.py` and `internal/places`. |
| [e2e](../e2e/README.md) | Native suites, fixture preparation, optional corpora and performance | Says which outside inputs a run can read, describes the `formats` fixture correctly, runs real-file Go tests one package at a time and links the screenshot capture. |
| [Installer acceptance](../e2e/installer/README.md) | Layout, extracted payload and offline Sandbox workflows | Added exact root-relative prerequisites and status link. Keeps local extraction separate from installed/clean-machine/signed acceptance. |

## History and experiments

| Material | Disposition |
| --- | --- |
| [Original plan](plan.md) | Historical design intent, not a living feature list. Its banner links to implemented deviations and the current feature reference. |
| [History index](history/README.md), phase records, audits and JSON reports | Retained as dated evidence. Index explains supersession and links to current owners; no past test result is rewritten as current acceptance. |
| [Spikes](../spikes/README.md) | Phase 0 experiments, separate Go module; not supported application setup. |
| [Updater spike](../spikes/updater/README.md) | Development-only trust experiment; not production trust or a feed deployment procedure. |

## Keeping documentation aligned

Read code and current manifests before copying a phase record. Dependency changes
need a check of Development, AGENTS, release status and screenshots. People changes
(including open [issue #30](https://github.com/awaisjameel/lumen/issues/30)) need
another walkthrough and native capture review; relationship/Me features are not
described as implemented before they exist.

Run `scripts/check-docs.sh` ([prerequisites](development.md#setup)) and the
[capture workflow](screenshots/README.md#reproducing-the-face-review). Keep offline
runtime policy consistent: application network use is limited to the explicitly
opt-in updater, inert without compiled trust; developer downloads are separate.
Do not infer signing or acceptance from a version, publication or passing local
test. Record remaining gates and link to their evidence.
