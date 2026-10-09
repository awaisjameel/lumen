// Drives the real Lumen app (production build, native WebView2) over the
// WebView's DevTools port, with a scratch library. Never touches the user's
// own library: LUMEN_DATA_DIR is always set.
import { execFileSync, spawn } from 'node:child_process'
import { existsSync, mkdirSync } from 'node:fs'
import puppeteer from 'puppeteer-core'

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** Starts exe on dataDir and connects to its WebView. */
export async function launch({ exe, dataDir, port, env = {}, args = [] }) {
  if (!dataDir) throw new Error('launch needs a scratch dataDir: the real library is never used')
  mkdirSync(dataDir, { recursive: true })
  const started = Date.now()
  const proc = spawn(exe, args, {
    // The AI stages run on battery too: a laptop's tests must not wait for mains.
    env: {
      ...process.env,
      LUMEN_AI_ON_BATTERY: '1',
      ...env,
      LUMEN_DATA_DIR: dataDir,
      LUMEN_DEVTOOLS_PORT: String(port),
    },
    stdio: 'ignore',
    detached: false,
  })
  // A suite that throws must not leave its Lumen running.
  process.once('exit', () => proc.exitCode === null && kill(proc))
  let browser
  for (let i = 0; i < 300 && !browser; i++) {
    // A Lumen started while another is still closing on the same library exits at once.
    if (proc.exitCode !== null) throw new Error(`Lumen exited with ${proc.exitCode} before its window opened`)
    try {
      const targets = await fetch(`http://127.0.0.1:${port}/json/list`).then((r) => r.json())
      if (targets.some((t) => t.type === 'page' && t.url.startsWith('http')))
        browser = await puppeteer.connect({ browserURL: `http://127.0.0.1:${port}`, defaultViewport: null })
    } catch {
      /* not listening yet */
    }
    if (!browser) await sleep(100)
  }
  if (!browser) {
    kill(proc)
    throw new Error('the WebView did not open its DevTools port')
  }
  let page
  for (let i = 0; i < 100 && !page; i++) {
    page = (await browser.pages()).find((p) => p.url().startsWith('http'))
    if (!page) await sleep(100)
  }
  if (!page) {
    kill(proc)
    throw new Error('no app page')
  }
  return { proc, browser, page, started }
}

// Only this run's own process tree: never another Lumen on the PC.
function kill(proc) {
  try {
    execFileSync('taskkill', ['/PID', String(proc.pid), '/T', '/F'], { stdio: 'ignore' })
  } catch {
    /* gone */
  }
}

/** Closes the app gracefully (no /F), then waits for it to exit. */
export async function close(app) {
  try {
    await app.browser.disconnect()
  } catch {
    /* already gone */
  }
  try {
    execFileSync('taskkill', ['/PID', String(app.proc.pid)], { stdio: 'ignore' })
  } catch {
    /* already gone */
  }
  for (let i = 0; i < 100 && app.proc.exitCode === null; i++) await sleep(100)
  if (app.proc.exitCode === null) kill(app.proc)
  await sleep(500)
}

/** Calls a bound service method by name from the page, as the bridge does. */
export function call(page, method, ...args) {
  return page.evaluate(
    async (method, args) => {
      const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`
      const r = await fetch('/wails/runtime', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'x-wails-client-id': `e2e-${id}` },
        body: JSON.stringify({ object: 0, method: 0, args: { 'call-id': id, methodName: method, args } }),
      })
      const text = await r.text()
      if (!r.ok) throw new Error(`${method}: ${text}`)
      try {
        return JSON.parse(text)
      } catch {
        return text
      }
    },
    method,
    args,
  )
}

const service =
  (name) =>
  (page, method, ...args) =>
    call(page, `lumen/services.${name}.${method}`, ...args)
export const compression = service('CompressionService')
export const lib = service('LibraryService')
export const media = service('MediaService')
export const search = service('SearchService')
export const people = service('PeopleService')
export const system = service('SystemService')

/** Lets indexing run whatever else the PC is doing, as a test needs. */
export async function unthrottle(page) {
  await lib(page, 'SetPauseBusy', false)
  await lib(page, 'SetIdleOnly', false)
}

/** Waits until indexing has nothing left: metadata and thumbnails, and with ai the embeddings and faces too. */
export async function waitIndexed(page, { timeout = 1_800_000, ai = false, atLeast = 1, log } = {}) {
  const start = Date.now()
  let last = ''
  for (;;) {
    const p = await lib(page, 'Progress')
    const left = (p.metadata ?? 0) + (p.thumbnail ?? 0) + (ai ? (p.embedding ?? 0) + (p.faces ?? 0) : 0)
    if (log) {
      const line = `scan=${p.scanning} total=${p.total} meta=${p.metadata} thumb=${p.thumbnail} embed=${p.embedding} faces=${p.faces} errors=${p.errors}`
      if (line !== last) log(`${((Date.now() - start) / 1000).toFixed(0)}s ${line}`)
      last = line
    }
    if (!p.scanning && left === 0 && p.total >= atLeast) return p
    if (Date.now() - start > timeout) throw new Error(`indexing did not finish: ${JSON.stringify(p)}`)
    await sleep(1000)
  }
}

export function ensureDir(dir) {
  if (!existsSync(dir)) mkdirSync(dir, { recursive: true })
  return dir
}

/** Sets the window's inner size through the DevTools protocol. */
export async function resize(page, width, height) {
  const session = await page.createCDPSession()
  const { windowId } = await session.send('Browser.getWindowForTarget')
  await session.send('Browser.setWindowBounds', { windowId, bounds: { windowState: 'normal' } })
  await session.send('Browser.setWindowBounds', { windowId, bounds: { width, height } })
  await session.detach()
  await sleep(400)
}

/** Moves the app's router to path, as a link would, without reloading the page. */
export async function go(page, path, wait = 1000) {
  await page.evaluate((path) => {
    history.pushState({}, '', path)
    window.dispatchEvent(new PopStateEvent('popstate'))
  }, path)
  await sleep(wait)
}

/** Usable: the grid shows at least a screen's first thumbnails, decoded. */
export function usableGrid(page, timeout = 30000) {
  return page.waitForFunction(
    () => {
      const imgs = [...document.querySelectorAll('.scroller .cell img')]
      return imgs.length >= 8 && imgs.slice(0, 8).every((i) => i.complete && i.naturalWidth > 0)
    },
    { timeout, polling: 16 },
  )
}

/** Memory of the app and its WebView2 processes (the app's process tree). */
export function memoryMB(pid) {
  const script = `
$all = Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId
$ids = New-Object System.Collections.Generic.List[int]
$ids.Add(${pid})
for ($i = 0; $i -lt $ids.Count; $i++) { foreach ($p in $all) { if ($p.ParentProcessId -eq $ids[$i] -and -not $ids.Contains([int]$p.ProcessId)) { $ids.Add([int]$p.ProcessId) } } }
$sum = 0; $priv = 0
foreach ($id in $ids) { $p = Get-Process -Id $id -ErrorAction SilentlyContinue; if ($p) { $sum += $p.WorkingSet64; $priv += $p.PrivateMemorySize64 } }
"$([math]::Round($sum/1MB)) $([math]::Round($priv/1MB)) $($ids.Count)"`
  const [ws, priv, n] = execFileSync('powershell', ['-NoProfile', '-Command', script])
    .toString()
    .trim()
    .split(' ')
    .map(Number)
  return { workingSetMB: ws, privateMB: priv, processes: n }
}
