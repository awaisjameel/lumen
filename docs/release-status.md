# Release status

Snapshot checked on **26 November 2026**, after v0.14.0 was published from
`e1cb2c7`. Recheck this page before publishing or changing the version. The
version's canonical source is `build/config.yml`; trust is configured only in
`internal/update/release-config.json`.

| Evidence | What it establishes | What it does not establish |
| --- | --- | --- |
| [Published v0.14.0](https://github.com/awaisjameel/lumen/releases/tag/v0.14.0), published 25 November at 19:58 UTC (00:58 on 26 November in Pakistan), not draft and not marked pre-release (the owner cleared the flag the workflow sets on unsigned drafts) | The release workflow built the installer, runtime ZIP, checksums and notices from the tag, after CI's checks passed, and the owner published them. One native suite failed on the first run, before Lumen's window opened; it passed when rerun, and the same suite passed locally against the run's own `Lumen.exe`. It carries Me and relationships in People (#30, #70) and dependency updates. Its notes say the build is unsigned; GitHub holds signed build provenance for the installer. | Code signing, clean-machine acceptance or an installed upgrade from an earlier version. |
| [Published v0.13.0](https://github.com/awaisjameel/lumen/releases/tag/v0.13.0), published 18 November at 18:57 UTC (23:57 in Pakistan) | The previous unsigned release, superseded by v0.14.0. Its files stay as published. | Anything about later source. |
| [Published v0.12.1](https://github.com/awaisjameel/lumen/releases/tag/v0.12.1), published 23 October at 21:20 UTC (24 October in Pakistan) | An earlier unsigned release, superseded by v0.13.0. Its files stay as published. | Anything about later source. |
| [Published v0.12.0](https://github.com/awaisjameel/lumen/releases/tag/v0.12.0), published 20 October at 21:31 UTC (21 October in Pakistan) | An earlier unsigned release, superseded by v0.12.1. Its files stay as published. | Anything about later source. |
| [Historical 0.11.0 acceptance](history/beta-package-acceptance-2026-10-10.md) and [updater implementation record](history/phase12-implementation-2026-10-12.md) | Dated evidence for the earlier beta; the latter records owner acceptance on 12 October. | Acceptance of the current source or a signed installed update. |
| Source at v0.14.0, with the [Me and relationships record](history/people-relationships-2026-11-25.md), the [PR 56 integration audit](history/pr56-integration-audit-2026-11-06.md) and the [23 October dependency record](history/dependency-upgrade-2026-10-23.md) | Hosted CI, including the large-library latency gate and the native suites; the relationships suite and before-and-after performance against v0.13.0's source, recorded on 25 November; and the earlier runtime import and extracted-payload evidence, including app-local Visual C++ DLLs. The shipped native runtime is unchanged since; only development tools were added. | Acceptance of the published installer on a clean machine. |
| Empty updater trust configuration | Repository builds cannot perform public update checks or delivery, even though the update implementation exists. | A configured publisher, hosted trusted feed or active automatic delivery. |

At this check the v0.14.0 installer's GitHub asset SHA-256 was
`f17a6fa7244b742b77248b0cab361dc0983ba585bb875aece0dd7b3ab227bd29`,
matching the release's `SHA256SUMS.txt` (v0.13.0's was
`bb974b1f58ae9d045af78af7b7b5b0d7ebb3f145ea5b0906758e8b8f9e830604`,
v0.12.1's `491101b3e478e1ab2cc32806c51737102f64bc6c723d04fb23f18abe4f126c45` and
v0.12.0's `3fbafd9de59a0bd54e312c54ee4ba9633533766d35dce43d8f7eab868f91bd13`).
Always compare the downloaded artifact with that release's own
`SHA256SUMS.txt`; do not substitute a locally rebuilt file.

The [24 October documentation verification](history/documentation-refresh-2026-10-24.md)
records the documentation checks, a local build and the face-review capture.
It does not change which assets are published or close the gates below.

## Outstanding release gates

- Clean installed upgrade from the accepted flat 0.11.0 package, reinstall and
  uninstall on the supported Windows matrix. The dependency run could not start
  Sandbox acceptance because the previous setup executable was unavailable.
- Clean-machine AI startup with the shipped Visual C++ runtime; local import and
  payload checks are useful evidence but do not replace this.
- Real-face corpus, additional GPU/hardware and camera-quality acceptance.
- Public Authenticode identity, timestamps and SmartScreen observation, production
  trust custody/feed hosting, and two genuinely signed installed updater releases.

The procedures and acceptance requirements are in [installer acceptance](../e2e/installer/README.md)
and [production signing](production-signing.md). The next publication needs a
new version under [the release checklist](releasing.md); published assets are
never replaced. Each screenshot shows the interface of the build it was
captured from ([screenshots](screenshots/README.md)), not acceptance of an
installer that shares its version number.
