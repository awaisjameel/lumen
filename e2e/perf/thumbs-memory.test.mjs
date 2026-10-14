import assert from 'node:assert/strict'
import test from 'node:test'
import { sampleWebviewMemory, webviewMemory } from './thumbs-memory.mjs'

test('Memory includes every scratch WebView descendant and excludes other applications.', () => {
  const row = (pid, parentPID, name, privateBytes, workingSetBytes) => ({
    pid,
    parentPID,
    name,
    privateBytes,
    workingSetBytes,
  })
  const processes = [
    row(4, 3, 'msedgewebview2.exe', 20, 40),
    row(3, 2, 'helper.exe', 1000, 2000),
    row(2, 1, 'msedgewebview2.exe', 10, 30),
    row(1, 99, 'Lumen.exe', 5000, 6000),
    row(5, 99, 'msedgewebview2.exe', 7000, 8000),
  ]
  const result = webviewMemory(processes, 1)
  assert.deepEqual(
    result.processes.map((process) => process.pid),
    [4, 2],
  )
  assert.equal(result.privateBytes, 30)
  assert.equal(result.workingSetBytes, 70)
})

test('A missing scratch WebView fails instead of reporting zero memory.', () => {
  assert.throws(() => webviewMemory([], 1), /no WebView/)
})

test('An invalid process id fails before starting the memory sampler.', async () => {
  for (const pid of [0, -1, 1.5, NaN, '1']) await assert.rejects(sampleWebviewMemory(pid), /invalid scratch process id/)
})
