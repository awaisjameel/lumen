# Optional read-only diagnostics for a slow Sandbox acceptance run.
if ($env:USERNAME -ne 'WDAGUtilityAccount') { throw 'This script only runs in Windows Sandbox.' }
@{
    at = [DateTime]::UtcNow.ToString('o')
    processes = @(Get-Process | Select-Object Name, Id, CPU, MainWindowTitle)
    installed = @(Get-ChildItem 'C:\Beta acceptance' -Recurse -File -ErrorAction SilentlyContinue | Select-Object FullName, Length, LastWriteTime)
    webview = @(Get-ChildItem 'C:\Program Files (x86)\Microsoft\EdgeWebView' -ErrorAction SilentlyContinue | Select-Object Name)
} | ConvertTo-Json -Depth 6 | Set-Content C:\LumenResults\status.json -Encoding UTF8
