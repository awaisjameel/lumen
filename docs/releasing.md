# Releasing Lumen

A release is a version bump merged into `main`, then a tag that the release workflow builds into a draft for review:

1. On an up-to-date `main`, run `scripts/version.sh bump 0.13.0`. It moves to a `release/v0.13.0` branch, sets the version in `build/config.yml`, regenerates the build assets (restoring the nfpm licence), runs `TestVersionsAgree` and `TestLicenceAndCopyrightAgree`, and commits `chore(release): Lumen 0.13.0`. Push the branch, open a pull request and merge it once CI passes.
2. On `main`, with that merge pulled, run `scripts/version.sh tag`. It refuses unless `main` matches `origin/main` and the version is untagged, then pushes the annotated tag `v0.13.0`.
3. The tag starts [the release workflow](../.github/workflows/release.yml), which:
   - checks the tag matches `build/config.yml` and sits on `main`;
   - runs CI's checks;
   - builds the installer, signed when signing is configured;
   - creates a **draft** release with the installer, the runtime archive, `THIRD_PARTY_NOTICES.txt`, `SHA256SUMS.txt` and build provenance when the repository is public (`gh attestation verify`).

   The workflow marks an unsigned draft as a pre-release, and its notes tell users how to check the download.

4. Review the draft: the generated notes, the files, and the installer on a test PC. Then publish it. Published files are never replaced: a fix is a new version. Turning on GitHub's immutable releases (Settings → General → Releases) enforces that.

Signing in the workflow switches on with the repository variables `SIGN_THUMBPRINT` and `TIMESTAMP_SERVER`, once a signing provider's setup step is added ([production signing](production-signing.md#signing-in-the-release-workflow)). The updater's authenticated feed is a separate, local step with offline keys, described in the same document; the release workflow never changes the updater's trust configuration.

## Publication and acceptance are separate

Read [release status](release-status.md) before describing a build as accepted.
The workflow creates a draft; it does not publish it, accept an installer or
enable update delivery. An owner can publish an unsigned release and change its
GitHub pre-release flag. That flag proves neither signing nor installer acceptance.
Never replace a published release's assets with a newer build that still has
its version in the configuration. Bump the version for the next release.

The signing, trust provisioning, feed and signed-upgrade gates have one owner:
[production signing and update delivery](production-signing.md). The executable
and installer acceptance commands live in [the installer guide](../e2e/installer/README.md).
