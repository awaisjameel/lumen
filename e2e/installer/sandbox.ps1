# Runs only in disposable Windows Sandbox, with no network adapter.
param(
    [string]$Inputs = 'C:\LumenInputs',
    [string]$Results = 'C:\LumenResults'
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'cdp.ps1')
if ($env:USERNAME -ne 'WDAGUtilityAccount') {
    throw 'Installer acceptance requires an offline Windows Sandbox.'
}
$adapters = [System.Net.NetworkInformation.NetworkInterface]::GetAllNetworkInterfaces() | Where-Object {
    $_.NetworkInterfaceType -ne [System.Net.NetworkInformation.NetworkInterfaceType]::Loopback -and
    $_.OperationalStatus -eq [System.Net.NetworkInformation.OperationalStatus]::Up
}
if ($adapters) { throw 'Installer acceptance refuses an active network adapter.' }
$checks = [System.Collections.Generic.List[object]]::new()
$app = $null
$install = 'C:\Beta acceptance\' + [char]0x00c9 + 't' + [char]0x00e9 + ' Lumen'
$photos = 'C:\Beta acceptance\Original photos'
$data = Join-Path $env:LOCALAPPDATA 'Lumen'
$registry = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Lumen contributorsLumen'
$port = 9498
$socket = $null
$manifest = Get-Content -LiteralPath (Join-Path $Inputs 'expected.json') -Raw | ConvertFrom-Json
New-Item -ItemType Directory -Path $Results, $photos -Force | Out-Null
Start-Transcript -Path (Join-Path $Results 'transcript.txt') -Force | Out-Null

function Check([string]$Name, [bool]$Pass, $Detail = '') {
    $checks.Add(@{ name = $Name; pass = $Pass; detail = $Detail })
    @{ environment = 'offline Windows Sandbox'; version = $manifest.version; checks = @($checks.ToArray()) } |
        ConvertTo-Json -Depth 12 | Set-Content -LiteralPath (Join-Path $Results 'report.json') -Encoding UTF8
    Write-Host "$Pass $Name $Detail"
    if (-not $Pass) { throw $Name }
}

function Setup([string]$Name) {
    # NSIS requires /D to be the final argument, with no enclosing quotes.
    $process = Start-Process -FilePath (Join-Path $Inputs $Name) -ArgumentList "/S /D=$install" -PassThru
    if (-not $process.WaitForExit(600000)) { throw 'Setup exceeded ten minutes.' }
    return $process.ExitCode
}

function Start-Lumen {
    $env:LUMEN_DATA_DIR = $data
    $env:LUMEN_DEVTOOLS_PORT = "$port"
    $env:LUMEN_AI_ON_BATTERY = '1'
    Remove-Item Env:LUMEN_RUNTIME_DIR -ErrorAction SilentlyContinue
    $script:app = Start-Process -FilePath (Join-Path $install 'Lumen.exe') -PassThru
    $readyDeadline = [DateTime]::UtcNow.AddSeconds(90)
    while ([DateTime]::UtcNow -lt $readyDeadline) {
        if ($app.HasExited) { throw "Lumen exited before readiness: $($app.ExitCode)" }
        try {
            $targets = Invoke-RestMethod "http://127.0.0.1:$port/json/list" -TimeoutSec 2
            $target = $targets | Where-Object { $_.type -eq 'page' -and $_.url.StartsWith('http') } | Select-Object -First 1
            if ($target) {
                $script:socket = Connect-DevTools $target.webSocketDebuggerUrl
                return
            }
        } catch { Write-Verbose $_ }
        Start-Sleep -Milliseconds 100
    }
    throw 'The installed WebView did not become ready.'
}

function Call([string]$Service, [string]$Method, [object[]]$Arguments = @()) {
    return Call-DevTools $socket $Service $Method $Arguments
}

function Close-Lumen {
    if ($socket) { $socket.Dispose(); $script:socket = $null }
    if ($app -and -not $app.HasExited) {
        & taskkill.exe /PID $app.Id | Out-Null
        if (-not $app.WaitForExit(60000)) { throw 'Lumen did not shut down gracefully.' }
    }
    $script:app = $null
    Start-Sleep -Seconds 2
}

function Verify-Payload {
    foreach ($entry in $manifest.files) {
        $path = Join-Path $install $entry.path
        Check "installed payload $($entry.path)" ((Test-Path -LiteralPath $path) -and (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -eq $entry.sha256)
    }
    $registered = Get-ItemProperty -LiteralPath $registry
    Check 'registered version and custom installation path' ($registered.DisplayVersion -eq $manifest.version -and $registered.InstallLocation -eq $install)
    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut((Join-Path ([Environment]::GetFolderPath('Programs')) 'Lumen.lnk'))
    Check 'Start menu shortcut targets the installed executable' ($shortcut.TargetPath -eq (Join-Path $install 'Lumen.exe'))
}

function Verify-Library {
    Start-Lumen
    $about = Call 'SystemService' 'About'
    Check 'installed application opens the same library at the intended version' ($about.version -eq $manifest.version -and $about.dataDir -eq $data) $about
    $sources = @(Call 'LibraryService' 'Sources')
    $preferences = Call 'LibraryService' 'Preferences'
    $albums = @(Call 'LibraryService' 'Albums')
    Check 'source folder, preference and album survive' ($sources.Count -eq 1 -and $sources[0].path -eq $photos -and $preferences.theme -eq 'dark' -and @($albums | Where-Object name -eq 'Preserved beta album').Count -eq 1)
    $asset = Call 'LibraryService' 'Asset' @(1)
    $album = $albums | Where-Object name -eq 'Preserved beta album' | Select-Object -First 1
    Check 'favourite and album membership survive' ($asset.favourite -and $album.count -eq 1)
    Close-Lumen
}

function Uninstall {
    # _?= runs the test's uninstaller in place, so waiting observes completion.
    $process = Start-Process -FilePath (Join-Path $install 'uninstall.exe') -ArgumentList "/S _?=$install" -PassThru
    if (-not $process.WaitForExit(120000)) { throw 'Uninstall exceeded two minutes.' }
    Check 'silent uninstall succeeds' ($process.ExitCode -eq 0) $process.ExitCode
    Check 'silent uninstall keeps the library and originals' ((Test-Path -LiteralPath (Join-Path $data 'library.db')) -and (Get-FileHash -LiteralPath $photo).Hash -eq $photoHash)
    Check 'uninstall preserves unrelated files' ((Get-Content -LiteralPath $foreign -Raw) -eq 'keep this unrelated file')
    Check 'uninstall removes owned runtime and registration' (-not (Test-Path -LiteralPath (Join-Path $install 'Lumen.exe')) -and -not (Test-Path -LiteralPath $registry) -and -not (Test-Path -LiteralPath (Join-Path $install 'compress\cjxl.exe')))
}

try {
    Check 'clean Windows account has no Lumen installation or library' (-not (Test-Path -LiteralPath $registry) -and -not (Test-Path -LiteralPath $data))
    Add-Type -AssemblyName System.Drawing
    $photo = Join-Path $photos 'original.jpg'
    $bitmap = New-Object System.Drawing.Bitmap 640, 480
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    $graphics.Clear([System.Drawing.Color]::CornflowerBlue)
    $bitmap.Save($photo, [System.Drawing.Imaging.ImageFormat]::Jpeg)
    $graphics.Dispose()
    $bitmap.Dispose()
    $photoHash = (Get-FileHash -LiteralPath $photo).Hash
    Check 'previous beta installs without network' ((Setup $manifest.previousInstaller) -eq 0)
    Start-Lumen
    Check 'previous beta starts offline' ((Call 'SystemService' 'About').version -eq $manifest.previousVersion)
    Call 'LibraryService' 'SetPauseBusy' @($false) | Out-Null
    Call 'LibraryService' 'SetPauseFullScreen' @($false) | Out-Null
    Call 'LibraryService' 'SetIdleOnly' @($false) | Out-Null
    Call 'LibraryService' 'AddSources' (, @($photos)) | Out-Null
    $indexed = $false
    for ($i = 0; $i -lt 180; $i++) {
        $progress = Call 'LibraryService' 'Progress'
        if ($progress.total -eq 1 -and $progress.metadata -eq 0 -and $progress.thumbnail -eq 0) { $indexed = $true; break }
        Start-Sleep -Milliseconds 500
    }
    Check 'installed previous beta indexes the original photo offline' $indexed
    Call 'LibraryService' 'SetPaused' @($true) | Out-Null
    Call 'LibraryService' 'SetFavourite' @(1, $true) | Out-Null
    $preferences = Call 'LibraryService' 'Preferences'
    $preferences.theme = 'dark'
    Call 'LibraryService' 'SetPreferences' @($preferences) | Out-Null
    Call 'LibraryService' 'CreateAlbum' @('Preserved beta album', 'manual', @{ kind = ''; favourites = $false; from = ''; to = ''; tag = 0; person = 0 }, @(1)) | Out-Null
    Close-Lumen
    $foreign = Join-Path $install 'unrelated.txt'
    [System.IO.File]::WriteAllText($foreign, 'keep this unrelated file')
    Check 'current beta over-installs the previous beta' ((Setup $manifest.installer) -eq 0)
    Verify-Payload
    Verify-Library
    Start-Lumen
    Check 'Setup refuses a running installed application' ((Setup $manifest.installer) -eq 2)
    $uninstaller = Start-Process -FilePath (Join-Path $install 'uninstall.exe') -ArgumentList "/S _?=$install" -PassThru
    if (-not $uninstaller.WaitForExit(120000)) { throw 'Locked uninstall exceeded two minutes.' }
    Check 'uninstall refuses a running installed application' ($uninstaller.ExitCode -eq 2)
    Close-Lumen
    Check 'same-version reinstall succeeds' ((Setup $manifest.installer) -eq 0)
    Verify-Payload
    Verify-Library
    Uninstall
    Check 'installation after uninstall succeeds' ((Setup $manifest.installer) -eq 0)
    Verify-Library
    Uninstall
    Check 'original photo bytes remain unchanged' ((Get-FileHash -LiteralPath $photo).Hash -eq $photoHash)
    [System.IO.File]::WriteAllText((Join-Path $Results 'complete.txt'), 'passed')
} catch {
    $checks.Add(@{ name = 'acceptance completed'; pass = $false; detail = $_.ToString() })
    @{ environment = 'offline Windows Sandbox'; version = $manifest.version; checks = @($checks.ToArray()) } |
        ConvertTo-Json -Depth 12 | Set-Content -LiteralPath (Join-Path $Results 'report.json') -Encoding UTF8
    [System.IO.File]::WriteAllText((Join-Path $Results 'complete.txt'), $_.ToString())
    Write-Error $_ -ErrorAction Continue
} finally {
    Close-Lumen
    if (Test-Path -LiteralPath $data) { Copy-Item -LiteralPath $data -Destination (Join-Path $Results 'library') -Recurse -Force }
    Stop-Transcript | Out-Null
    # Shutdown discards the container after the mapped report has been flushed.
    & shutdown.exe /s /t 5
}
