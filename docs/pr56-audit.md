# PR #56 integration audit

Audit of [PR #56](https://github.com/awaisjameel/lumen/pull/56), begun on
7 November 2026 (Asia/Karachi). The initial head was
[`e09f966`](https://github.com/awaisjameel/lumen/commit/e09f96639a67011b4743be958dd943d6440fea3c)
and the current base was
[`bae949c`](https://github.com/awaisjameel/lumen/commit/bae949c3e2d4d1b9342b63b103917f9621c17ed7).
The original working tree was clean, with one worktree. All eleven children
were already integrated; no review comments or review threads were present.
Fixes remain on `feat/31-feature-completion`. Nothing is merged or released;
[#31](https://github.com/awaisjameel/lumen/issues/31) remains open.

## Confirmed findings and evidence

| Finding and impact | Origin and root cause | Repair and regression evidence | Acceptance still needed |
| --- | --- | --- | --- |
| **High: membership Undo could modify a blocked album.** A visible photo could be added to or removed from a label-blocked album, including after an inverse was retained for retry. | Existing single membership admission checked album kind and asset presence; the integrated batch/recovery path reused it without checking album visibility. | `setAlbumItem` now checks the canonical album blocking predicate inside its transaction. The before regression failed for both add and remove; the repaired store race regression verifies unchanged membership on refusal and successful retry after unblocking. Native `undo-recovery` now exercises real header/inspector controls and the PIN gate. | Final Windows native result. |
| **Medium: single-photo favourite Undo overwrote newer explicit intent.** A later same-value batch confirmation did not protect the photo from an older single-control inverse. | Existing single-control Undo acquired a new write lease at activation, bypassing the PR's shared ownership retained by batch Undo. | Single and batch controls now share the bounded favourite inverse owner. The before regression observed the incorrect `SetFavourite(false)`. Regressions cover supersession and blocked-target retry after navigation without changing the current selection. Native `favourite-undo` now checks the single-control versus newer batch case. | Final Windows native result. |
| **Medium: committed collection changes lost Undo on refresh failure; late saves changed a replacement header.** | Header code treated saving and refreshing as one operation, and local completion/error state had no collection lifetime guard. The PR's persistent metadata/draft work exposed this existing owner mismatch. | Names and pins retain their committed notification before refresh. Captured collection refresh owners and synchronous lifetime retirement prevent an old save from closing a new draft, clearing its busy state or showing an error in another collection. Two before regressions failed; all 19 header tests pass, including a newer pending save. Native `album-draft` adds a real committed header save followed by an injected read failure and Undo. | Final Windows native result. |
| **Medium: acceptance could hide unexpected resource failures.** | The existing shared harness and two new fault suites blanket-filtered `Failed to load resource`. Thus unrelated bridge/media failures could disappear from a green report. | Browser errors are attributed to current generated method identities and scenario context. Only exact expected refusals account for their individual resource messages; page errors always fail. Recorder regressions verify that unrelated methods, media failures, duplicate messages and errors outside the injection context remain failures. | Final native runs under stricter attribution. |

## Verification ledger

- Initial frontend baseline: **1,172 tests in 121 files passed**. This is fresh
  baseline evidence, not a final-head result.
- Favourite regression failed before the repair; focused store/selection/write
  ownership verification passed **72 tests** afterwards.
- Membership regression failed before the repair; focused album/batch race
  tests passed afterwards. The full Windows store shard then exposed an XMP
  trigger fixture using the newly prohibited API. Its setup now inserts the
  membership directly, like the adjacent event-membership trigger fixture;
  its original dirty-work assertions are retained. No privacy check is removed.
- Header regressions failed before the repair and **19 tests passed** afterwards.
- [Windows run 42276998439](https://github.com/awaisjameel/lumen/actions/runs/42276998439)
  at `73f7af6` passed production build/current bindings, the store-oracle and
  remaining-package race shards. Static checks failed on the new test's
  `void` generic; that type is corrected to `undefined`. The store shard failed
  on the XMP setup described above. Native tests and packaging were skipped.
  **This is failed intermediate evidence, not acceptance.**
- Final checks and detailed review coverage are being completed. No historical
  child pass or reporting-only latency measurement is counted as a final pass.

## External gates

This workspace is Linux. Windows production build, WebView2 controls and
unsigned packaging are verified through the repository's Windows CI, not by
pretending that a process launch or mocked component is native acceptance.
Only synthetic scratch libraries are used. The calibrated i7-13700H latency
gate, real corpus/hardware acceptance, production trust/signing, signed upgrade
and installed-machine acceptance remain separate requirements. The historical
181.786/70 ms hosted SaveEmbeddings64 result remains historical failed evidence;
budgets are unchanged. A hosted reporting-only pass does not close calibration.
