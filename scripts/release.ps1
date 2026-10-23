# Produces signed, locally reviewable artifacts. It never publishes a release.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('Package', 'Verify', 'Feed')][string]$Mode,
    [string]$CertificateThumbprint,
    [string]$TimestampServer,
    [string]$RootMetadata,
    [string]$MetadataUrl = 'https://awaisjameel.github.io/lumen/updates',
    [string[]]$AdditionalPublisherPins = @(),
    [string[]]$TargetsKeys = @(),
    [string]$SnapshotKey,
    [string]$TimestampKey,
    [long]$MetadataVersion,
    [string]$FeedOutput,
    [string]$AcceptanceReport,
    [string]$PreviousTargets
)

$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
Set-Location -LiteralPath $repoRoot

function Invoke-Checked([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}

function Assert-Signed([string]$Path, [string[]]$Pins) {
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne 'Valid' -or $null -eq $signature.TimeStamperCertificate) {
        throw "A valid timestamped signature is required: $Path"
    }
    $hash = [Security.Cryptography.SHA256]::Create()
    try { $pin = [BitConverter]::ToString($hash.ComputeHash($signature.SignerCertificate.RawData)).Replace('-', '').ToLowerInvariant() }
    finally { $hash.Dispose() }
    if ($Pins -notcontains $pin) { throw "The signer is outside the approved publisher policy: $Path" }
    Invoke-Checked 'signtool' @('verify', '/pa', '/all', '/tw', $Path)
}

$configFile = Join-Path $repoRoot 'internal\update\release-config.json'
if ($Mode -eq 'Package') {
    if (-not $CertificateThumbprint -or -not $TimestampServer -or -not $RootMetadata) {
        throw 'Package requires CertificateThumbprint, TimestampServer and RootMetadata.'
    }
    $timestampUri = [Uri]$TimestampServer
    $metadataUri = [Uri]$MetadataUrl
    if ($timestampUri.Scheme -ne 'https' -or $metadataUri.Scheme -ne 'https') { throw 'Release services require HTTPS.' }
    $certificate = Get-Item -LiteralPath "Cert:\CurrentUser\My\$CertificateThumbprint"
    if (-not $certificate.HasPrivateKey -or -not $certificate.Verify()) { throw 'A publicly trusted signing certificate with protected private-key access is required.' }
    $hash = [Security.Cryptography.SHA256]::Create()
    try { $pin = [BitConverter]::ToString($hash.ComputeHash($certificate.RawData)).Replace('-', '').ToLowerInvariant() }
    finally { $hash.Dispose() }
    $publicRoot = Get-Content -Raw -LiteralPath $RootMetadata | ConvertFrom-Json
    if ($publicRoot.signed.roles.root.threshold -lt 2 -or $publicRoot.signed.roles.targets.threshold -lt 2) {
        throw 'Production root and targets must require distinct multi-key quorums.'
    }
    $config = [ordered]@{
        root = $publicRoot
        metadataURL = $MetadataUrl.TrimEnd('/')
        targetsURL = 'https://github.com/awaisjameel/lumen/releases/download'
        origins = @($metadataUri.GetLeftPart([UriPartial]::Authority), 'https://github.com', 'https://release-assets.githubusercontent.com', 'https://objects.githubusercontent.com') | Select-Object -Unique
        publisherPins = @($pin) + $AdditionalPublisherPins
    }
    [IO.File]::WriteAllBytes($configFile, [Text.Encoding]::UTF8.GetBytes(($config | ConvertTo-Json -Depth 100) + "`n"))
    Invoke-Checked 'wails3' @('generate', 'bindings', '-f', '-tags production', '-clean=true', '-ts', '-i')
    Invoke-Checked 'C:\Program Files\Git\bin\bash.exe' @('-lc', 'source scripts/env.sh && scripts/lint.sh && go test -race -p 1 -count=1 -timeout 1800s . ./internal/... ./services/... ./cmd/... ./scripts/...')
    Invoke-Checked 'wails3' @('task', 'package', "SIGN_THUMBPRINT=$CertificateThumbprint", "TIMESTAMP_SERVER=$TimestampServer")
}

$config = Get-Content -Raw -LiteralPath $configFile | ConvertFrom-Json
if (-not $config.publisherPins -or -not $config.root) { throw 'The build has no reviewed production trust configuration.' }
$version = [regex]::Match([IO.File]::ReadAllText((Join-Path $repoRoot 'build\config.yml')), '(?m)^  version: "([^"]+)"').Groups[1].Value
if (-not $version) { throw 'Missing canonical application version.' }
foreach ($name in @('Lumen.exe', 'Lumen-launcher.exe', 'lumen-update.exe', "Lumen-$version-x64-setup.exe")) {
    Assert-Signed (Join-Path $repoRoot "bin\$name") $config.publisherPins
}

if ($Mode -eq 'Feed') {
    if (-not $RootMetadata -or -not $AcceptanceReport -or -not $FeedOutput -or $TargetsKeys.Count -lt 2 -or -not $SnapshotKey -or -not $TimestampKey -or $MetadataVersion -lt 1) {
        throw 'Feed requires two targets keys, snapshot/timestamp keys, an increasing version, a new output directory and installed acceptance evidence.'
    }
    $report = Get-Content -Raw -LiteralPath $AcceptanceReport | ConvertFrom-Json
    $installerHash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $repoRoot "bin\Lumen-$version-x64-setup.exe")).Hash.ToLowerInvariant()
    $payloadHash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $repoRoot "bin\Lumen-$version-windows-amd64-runtime.zip")).Hash.ToLowerInvariant()
    if ($report.version -ne $version -or $report.installerSHA256 -ne $installerHash -or $report.payloadSHA256 -ne $payloadHash -or -not $report.signedInstalledUpdatePassed -or -not $report.failureRecoveryPassed) {
        throw 'Installed signed-version and recovery evidence does not match these exact artifacts.'
    }
    $feedArguments = @('run', './cmd/lumen-feed', '--root', $RootMetadata, '--target', 'bin/update-target.json', '--payload', "bin/Lumen-$version-windows-amd64-runtime.zip", '--out', $FeedOutput, '--targets-keys', ($TargetsKeys -join ','), '--snapshot-key', $SnapshotKey, '--timestamp-key', $TimestampKey, '--metadata-version', [string]$MetadataVersion)
    if ($PreviousTargets) { $feedArguments += @('--previous-targets', $PreviousTargets) }
    Invoke-Checked 'go' $feedArguments
}
