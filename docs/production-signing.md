# Production signing and update delivery

The production process is implemented by `scripts/release.ps1`,
`cmd/lumen-trust`, `cmd/lumen-feed`, and the signed NSIS package task.
It prepares local artifacts; none of these commands publishes automatically.
The checkout's `internal/update/release-config.json` is deliberately empty.
There is no production trust root, signing identity or public feed in this
checkout. Fixture signing keys are not a production bootstrap.

## Provision the external signing identities

Obtain a publicly trusted Authenticode code-signing certificate for the
publisher's verified identity. Use a hardware-backed key or a protected cloud
signing provider that exposes a Windows certificate-store signing operation.
Install its public certificate in the signing account's `CurrentUser\My`
store and install the Windows SDK's SignTool. Never commit/export its private
key, PFX password or signing-service credentials. A self-signed certificate
does not meet the public-release gate.

The package command selects the certificate by thumbprint and requires an
HTTPS RFC 3161 timestamp service. It uses SHA-256 file/timestamp digests.
The updater pins SHA-256 of the authorised signer's DER certificate, in
addition to Windows signature validation and a timestamp. Review a renewal
certificate and add its public digest through `AdditionalPublisherPins` before
using it to sign updates for existing installations. Removing a compromised
publisher from future builds does not alter old compiled policies: withdraw
affected targets, use the TUF root recovery procedure and supply a signed
offline repair installer when an old publisher policy cannot accept renewal.

Generate the initial TUF root on an offline Windows signing workstation:

```powershell
go run ./cmd/lumen-trust --keys 'E:\Lumen custody\initial' --out 'E:\Lumen public\1.root.json'
```

Both directories must have existing parents. The private directory must be
new, absolute and outside the checkout. The tool disables inherited directory
access and grants only the current account and SYSTEM before writing keys.
It generates separate Ed25519 keys: root 2-of-3, targets 2-of-3, snapshot 1-of-1
and timestamp 1-of-1. It verifies the root quorum before writing public metadata.
Split the three root keys among separate offline custodians/devices after
creation; move targets keys to independently approved signing custody. Keep
snapshot/timestamp credentials separate, restricted and replaceable. The
directory ACL is protection for creation, not an HSM or an operational quorum.
Only the public root enters the compiled build configuration.

## Build and verify a signed release

Review `build/config.yml`'s version, release notes, public root and publisher
pins. Keep private keys outside the source tree and published artifacts.

```powershell
./scripts/release.ps1 -Mode Package `
  -CertificateThumbprint '<certificate-store thumbprint>' `
  -TimestampServer '<approved HTTPS RFC3161 endpoint>' `
  -RootMetadata 'E:\Lumen public\1.root.json'
```

This writes public trust configuration, regenerates bindings, runs the lint
and Go race gates, builds sequentially, signs the application, stable launcher,
independent helper, installer and NSIS-generated uninstaller, and verifies
the resulting executable signatures. The installer contains a complete
versioned runtime. The separate runtime ZIP omits WebView2, which the offline
installer already supplies. Inventory hashes are calculated after signing.

`-Mode Verify` repeats Windows publisher and timestamp checks without building.
Verify the installed uninstaller's signature in the disposable acceptance
environment too: the build's uninstaller is embedded in NSIS, not a standalone
artifact under `bin/`.

Run the extracted-payload/native suite and the offline installer matrix in a
clean environment permitting the signed packages. The first updater-capable
release must over-install the accepted flat 0.11.0 beta. Then use two genuinely
signed updater-capable releases to test requested download, Later across
restart, approved activation, all open libraries, WAL/schema recovery,
shortcuts/registration, uninstall, and interrupted durable transitions.
Do not report fixture signatures or extracted runtime checks as these gates.

Record acceptance for the exact artifacts in a reviewed report:

```json
{
  "version": "0.12.0",
  "installerSHA256": "<actual installer SHA-256>",
  "payloadSHA256": "<actual runtime ZIP SHA-256>",
  "signedInstalledUpdatePassed": true,
  "failureRecoveryPassed": true
}
```

The booleans summarise attached native/VM evidence; they are not instructions
to manufacture a passing report. The signing process refuses feed preparation
without matching hashes and both accepted results.

## Prepare and publish authenticated metadata

`bin/update-target.json` is generated from the complete runtime. Review its
plain-text notes, channel, schema read/write range, Windows/updater minimums,
inventory and immutable artifact URL before signing. Preserve each supported
upgrade path's schema policy; the generated default is deliberately the current
schema only. Do not loosen it without compatibility evidence.

```powershell
./scripts/release.ps1 -Mode Feed `
  -RootMetadata 'E:\Lumen public\1.root.json' `
  -TargetsKeys 'E:\Targets custody A\targets-1.pem','F:\Targets custody B\targets-2.pem' `
  -SnapshotKey 'E:\Online custody\snapshot-1.pem' `
  -TimestampKey 'F:\Online custody\timestamp-1.pem' `
  -MetadataVersion <next monotonic version> `
  -FeedOutput 'E:\Lumen public\feed-new' `
  -AcceptanceReport 'E:\Lumen evidence\accepted.json'
```

The feed output must not exist. The tool checks the runtime hash, verifies
each signing role against the reviewed root, and emits targets, snapshot and
timestamp metadata. Defaults: targets expire in 30 days, snapshot in 7 days,
timestamp in 24 hours. The application uses full TUF refresh and durable
accepted roots/versions; it never treats a checksum beside an unsigned download
as authentication.

Pass `PreviousTargets` when adding a release to an existing feed so the other
channel and supported old targets remain present. The tool verifies those
targets against the current root and refuses changing an existing target's
length/hash. Root-role rotation requires re-signing the preserved target
inventory under the new targets quorum before using it as this input.

Daily refresh needs only the online snapshot/timestamp keys while the existing
targets remain valid:

```powershell
go run ./cmd/lumen-feed --refresh --root 'E:\Lumen public\1.root.json' `
  --previous-targets 'E:\Lumen public\current\targets.json' `
  --snapshot-key 'E:\Online custody\snapshot-1.pem' `
  --timestamp-key 'F:\Online custody\timestamp-1.pem' `
  --metadata-version <next monotonic version> --out 'E:\Lumen public\refresh-new'
```

Targets expiry requires the targets quorum; online refresh cannot extend it.

Publication is a separate owner-authorised operation:

1. Upload the immutable signed installer and runtime ZIP to release tag
   `v<version>` in `awaisjameel/lumen`. Never replace bytes at an existing URL.
2. Configure static metadata hosting at the reviewed `MetadataUrl` (default
   `https://awaisjameel.github.io/lumen/updates`). This URL is a proposed endpoint;
   no Pages deployment or hosted feed was created in this implementation.
3. Publish versioned roots, targets and snapshots first; publish timestamp
   last, after checking all referenced lengths/hashes are reachable.
4. Keep each intermediate root and supported old payload. Refresh snapshot
   and timestamp daily with a strictly increasing metadata version even when
   there is no application release. Monitor expiry and publication failures.

The application permits only the compiled HTTPS origins, with bounded redirect
count and platform TLS verification. Release-asset redirects may include the
host's temporary signed query; the updater supplies no credentials or library
payload. New delivery hosts need a reviewed build policy change.

## Rotation, withdrawal and recovery

Create a new role set/root, signed by both the old and new root quorums:

```powershell
go run ./cmd/lumen-trust --keys 'E:\Lumen custody\rotation-2' `
  --out 'E:\Lumen public\2.root.json' `
  --previous-root 'E:\Lumen public\1.root.json' `
  --previous-keys 'E:\Root custodian A\root-1.pem,F:\Root custodian B\root-2.pem'
```

Publish `2.root.json` without deleting `1.root.json`; re-sign current targets,
snapshot and timestamp with the new role credentials and increasing versions.
Retain intermediate roots permanently for supported clients. Rotation replaces
the role authorities; a self-signed substitution is refused. Loss of the old
root quorum requires a separately verified offline installer, not a feed that
changes its own trust anchor.

Withdraw a bad release by removing its target in a newly quorum-signed targets
document and updating snapshot/timestamp. A prepared update is checked again
before approved restart. An unavailable/expired feed delays activation; it
does not authorise unsigned execution or remote downgrade. Before health
acceptance, recovery pairs the previous generation with its consistent library
backup. After the durable health checkpoint, automatic database restoration is
forbidden; use compatible runtime repair or an explicitly reviewed repair flow.

`cmd/lumen-feed --withdraw-target <exact target> --previous-targets <current
targets.json>` performs that removal with the root, both targets keys,
snapshot/timestamp keys, next metadata version and new output directory
supplied as above. It preserves every other authenticated target.

Primary references: [TUF client workflow](https://theupdateframework.github.io/specification/latest/#detailed-client-workflow),
[Microsoft SignTool](https://learn.microsoft.com/en-us/windows/win32/seccrypto/signtool)
and [Windows publisher verification](https://learn.microsoft.com/en-us/windows/win32/api/wintrust/nf-wintrust-winverifytrust).
