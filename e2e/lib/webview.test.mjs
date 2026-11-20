import { strict as assert } from 'node:assert'
import { EventEmitter } from 'node:events'
import { createServer } from 'node:http'
import { test } from 'node:test'
import { waitForAppPage } from './webview.mjs'

const appPage = { type: 'page', url: 'http://wails.localhost/' }
const blank = { type: 'page', url: 'about:blank' }

/** A stand-in for the spawned Lumen: still running until a test says otherwise. */
function running() {
  const proc = new EventEmitter()
  proc.exitCode = null
  return proc
}

/** A DevTools port whose /json/list answers with whatever answer returns; null leaves the request hanging. */
async function devtools(answer) {
  const sockets = new Set()
  const server = createServer((request, response) => {
    const targets = answer()
    if (targets === null) return
    response.setHeader('Content-Type', 'application/json')
    response.end(JSON.stringify(targets))
  })
  server.on('connection', (socket) => {
    sockets.add(socket)
    socket.on('close', () => sockets.delete(socket))
  })
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
  return {
    port: server.address().port,
    close: () =>
      new Promise((resolve) => {
        for (const socket of sockets) socket.destroy()
        server.close(resolve)
      }),
  }
}

test('waits while the WebView lists only a blank page and returns once the app page is listed', async () => {
  let asked = 0
  const port = await devtools(() => (++asked < 5 ? [blank] : [blank, appPage]))
  try {
    const targets = await waitForAppPage(running(), port.port, { deadline: 10_000 })
    assert.deepEqual(targets, [blank, appPage])
    assert.equal(asked, 5)
  } finally {
    await port.close()
  }
})

test('a port that accepts a request and never answers does not hold the wait past its deadline', async () => {
  const port = await devtools(() => null)
  try {
    const started = Date.now()
    await assert.rejects(
      waitForAppPage(running(), port.port, { deadline: 600, request: 200 }),
      /did not open its app page/,
    )
    assert.ok(Date.now() - started < 3_000, `the wait took ${Date.now() - started} ms`)
  } finally {
    await port.close()
  }
})

test('says what the port last listed when the deadline passes', async () => {
  const port = await devtools(() => [blank])
  try {
    await assert.rejects(waitForAppPage(running(), port.port, { deadline: 300 }), (error) => {
      assert.match(error.message, /within \d+ s: the port listed \["page about:blank"\]/)
      return true
    })
  } finally {
    await port.close()
  }
})

test('says the port never answered when nothing listens on it', async () => {
  const port = await devtools(() => [])
  await port.close()
  await assert.rejects(waitForAppPage(running(), port.port, { deadline: 300 }), /the port never answered/)
})

test('stops at once, without waiting for the deadline, when Lumen exits', async () => {
  const proc = running()
  proc.exitCode = 3
  const started = Date.now()
  await assert.rejects(waitForAppPage(proc, 9, { deadline: 60_000 }), /Lumen exited with 3 before its window opened/)
  assert.ok(Date.now() - started < 1_000)
})

test('stops at once, without waiting for the deadline, when Lumen cannot start', async () => {
  const port = await devtools(() => [])
  try {
    const proc = running()
    const waiting = waitForAppPage(proc, port.port, { deadline: 60_000 })
    proc.emit('error', new Error('spawn Lumen.exe ENOENT'))
    await assert.rejects(waiting, /Lumen did not start: spawn Lumen.exe ENOENT/)
  } finally {
    await port.close()
  }
})
