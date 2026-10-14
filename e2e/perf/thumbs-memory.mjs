import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

const runFile = promisify(execFile)

export function webviewMemory(processes, rootPID) {
  const descendants = new Set([rootPID])
  let previous = 0
  while (previous !== descendants.size) {
    previous = descendants.size
    for (const process of processes) if (descendants.has(process.parentPID)) descendants.add(process.pid)
  }
  const rows = processes.filter((process) => descendants.has(process.pid) && process.name === 'msedgewebview2.exe')
  if (!rows.length) throw new Error('no WebView processes descended from the scratch app')
  return {
    processes: rows,
    privateBytes: rows.reduce((sum, process) => sum + process.privateBytes, 0),
    workingSetBytes: rows.reduce((sum, process) => sum + process.workingSetBytes, 0),
  }
}

export async function sampleWebviewMemory(rootPID) {
  if (!Number.isSafeInteger(rootPID) || rootPID <= 0) throw new Error('invalid scratch process id')
  const { stdout } = await runFile(
    'powershell.exe',
    [
      '-NoProfile',
      '-NonInteractive',
      '-Command',
      '$rows = @(Get-CimInstance Win32_Process | ForEach-Object { $native = Get-Process -Id $_.ProcessId -ErrorAction SilentlyContinue; if ($native) { [pscustomobject]@{ pid = [int]$_.ProcessId; parentPID = [int]$_.ParentProcessId; name = $_.Name; privateBytes = $native.PrivateMemorySize64; workingSetBytes = $native.WorkingSet64 } } }); ConvertTo-Json -InputObject $rows -Compress',
    ],
    { windowsHide: true },
  )
  return webviewMemory(JSON.parse(stdout), rootPID)
}
