# Builds a disposable, offline installer acceptance environment.
param([string]$PreviousVersion = '0.11.0')
$ErrorActionPreference = 'Stop'
function Sandbox-Ids { return @(& wsb.exe list | Where-Object { $_ -match '^[a-f0-9-]{36}$' }) }
if (@(Sandbox-Ids).Count) { throw 'Close the existing Windows Sandbox before running installer acceptance.' }
$root = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$version = [regex]::Match((Get-Content (Join-Path $root 'build\config.yml') -Raw), '(?m)^\s*version:\s*"([^"]+)"').Groups[1].Value
if (-not $version -or $PreviousVersion -notmatch '^\d+\.\d+\.\d+$') { throw 'Invalid package versions.' }
$installer = "Lumen-$version-x64-setup.exe"
$previous = "Lumen-$PreviousVersion-x64-setup.exe"
$bin = Join-Path $root 'bin'
foreach ($name in @($installer, $previous)) {
    if (-not (Test-Path -LiteralPath (Join-Path $bin $name))) { throw "Missing $name; build both packages first." }
}
$run = Join-Path $root ('e2e\.work\installer-' + [guid]::NewGuid().ToString('N'))
$inputs = Join-Path $run 'inputs'
$results = Join-Path $run 'results'
New-Item -ItemType Directory -Path $inputs, $results -Force | Out-Null
# Hard links avoid copying two large installers; Sandbox maps them read-only.
foreach ($name in @($installer, $previous)) {
    New-Item -ItemType HardLink -Path (Join-Path $inputs $name) -Target (Join-Path $bin $name) | Out-Null
}
$runtime = Join-Path $root 'deps\runtime'
$packaged = Join-Path $bin 'update-package'
$versioned = Test-Path -LiteralPath (Join-Path $packaged '.lumen-active.json')
if ($versioned) { $runtime = $packaged }
$files = @(Get-ChildItem -LiteralPath $runtime -File -Recurse | ForEach-Object {
    @{ path = $_.FullName.Substring($runtime.Length + 1); sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash }
})
$files += @('Lumen.exe', 'THIRD_PARTY_NOTICES.txt') | ForEach-Object {
    $source = $_
    if ($versioned -and $_ -eq 'Lumen.exe') { $source = 'Lumen-launcher.exe' }
    @{ path = $_; sha256 = (Get-FileHash -LiteralPath (Join-Path $bin $source) -Algorithm SHA256).Hash }
}
$manifest = @{ version = $version; installer = $installer; previousVersion = $PreviousVersion; previousInstaller = $previous; files = $files }
[System.IO.File]::WriteAllText((Join-Path $inputs 'expected.json'), ($manifest | ConvertTo-Json -Depth 8), [System.Text.UTF8Encoding]::new($false))
function Xml([string]$Path) { return [System.Security.SecurityElement]::Escape($Path) }
$config = @"
<Configuration>
  <Networking>Disable</Networking>
  <ClipboardRedirection>Disable</ClipboardRedirection>
  <MemoryInMB>4096</MemoryInMB>
  <MappedFolders>
    <MappedFolder><HostFolder>$(Xml $inputs)</HostFolder><SandboxFolder>C:\LumenInputs</SandboxFolder><ReadOnly>true</ReadOnly></MappedFolder>
    <MappedFolder><HostFolder>$(Xml $PSScriptRoot)</HostFolder><SandboxFolder>C:\LumenScripts</SandboxFolder><ReadOnly>true</ReadOnly></MappedFolder>
    <MappedFolder><HostFolder>$(Xml $results)</HostFolder><SandboxFolder>C:\LumenResults</SandboxFolder><ReadOnly>false</ReadOnly></MappedFolder>
  </MappedFolders>
  <LogonCommand><Command>powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\LumenScripts\bootstrap.ps1</Command></LogonCommand>
</Configuration>
"@
$wsb = Join-Path $run 'acceptance.wsb'
[System.IO.File]::WriteAllText($wsb, $config, [System.Text.UTF8Encoding]::new($false))
Write-Output "Installer results: $results"
$sandbox = Start-Process -FilePath (Join-Path $env:WINDIR 'System32\WindowsSandbox.exe') -ArgumentList $wsb -WindowStyle Hidden -PassThru
$sandboxId = $null
$startupDeadline = [DateTime]::UtcNow.AddMinutes(2)
while (-not $sandboxId -and [DateTime]::UtcNow -lt $startupDeadline) {
    $instances = @(Sandbox-Ids)
    if ($instances.Count -eq 1 -and $instances[0] -match '^[a-f0-9-]{36}$') { $sandboxId = $instances[0] }
    if (-not $sandboxId) { Start-Sleep -Seconds 2 }
}
if (-not $sandboxId) { throw "Sandbox did not start; inspect $results." }
Write-Output "Sandbox instance: $sandboxId"
$deadline = [DateTime]::UtcNow.AddMinutes(40)
$lastCount = -1
while (-not (Test-Path -LiteralPath (Join-Path $results 'complete.txt'))) {
    if ([DateTime]::UtcNow -gt $deadline) { throw "Sandbox acceptance timed out; inspect $results." }
    if (Test-Path -LiteralPath (Join-Path $results 'report.json')) {
        try {
            $report = Get-Content -LiteralPath (Join-Path $results 'report.json') -Raw | ConvertFrom-Json
            if ($report.checks.Count -ne $lastCount) {
                $lastCount = $report.checks.Count
                Write-Output "Sandbox: $lastCount checks; $($report.checks[-1].name)"
            }
        } catch { Write-Verbose $_ }
    }
    if ($sandbox.HasExited -and $sandbox.ExitCode -ne 0) { throw "Windows Sandbox failed: $($sandbox.ExitCode); inspect $results." }
    if (@(Sandbox-Ids) -notcontains $sandboxId) { throw "Sandbox exited before completing acceptance; inspect $results." }
    Start-Sleep -Seconds 5
}
$report = Get-Content -LiteralPath (Join-Path $results 'report.json') -Raw | ConvertFrom-Json
$failed = @($report.checks | Where-Object { -not $_.pass })
Write-Output "Sandbox: $($report.checks.Count - $failed.Count)/$($report.checks.Count) passed. Results: $results"
if ($failed.Count) { throw ($failed | ConvertTo-Json -Depth 8) }
