# Preserve startup errors as well as the acceptance report in the mapped output.
$ErrorActionPreference = 'Stop'
if ($env:USERNAME -ne 'WDAGUtilityAccount') { throw 'This script only runs in Windows Sandbox.' }
try {
    & C:\LumenScripts\sandbox.ps1
} catch {
    $failure = @{ environment = 'Windows Sandbox bootstrap'; checks = @(@{ name = 'sandbox startup'; pass = $false; detail = $_.ToString() }) }
    $failure | ConvertTo-Json -Depth 8 | Set-Content C:\LumenResults\report.json -Encoding UTF8
    [System.IO.File]::WriteAllText('C:\LumenResults\complete.txt', $_.ToString())
    & shutdown.exe /s /t 5
}
