// Measures one build on an indexed scratch library: cold start to a usable
// grid, idle memory, scroll frame times, first and warm search, the viewer,
// and memory after use. Run on mains power: a battery throttles everything.
//   node perf/perf.mjs [label] [exe] [dataDir]
// Without exe and dataDir it measures the build under test (bin/Lumen.exe or
// LUMEN_EXE) on a copy of the base library. LUMEN_E2E_PHOTOS=3000 makes that
// library the size the published figures were measured on.
import { mkdirSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { close, launch, memoryMB, sleep, usableGrid } from '../lib/app.mjs'
import { libraryCopy } from '../lib/fixtures.mjs'
import { EXE, PERF_PORT, WORK } from '../lib/suite.mjs'

const [label = 'current', exeArg, dataArg] = process.argv.slice(2)
const exe = exeArg ? resolve(exeArg) : EXE
const dataDir = dataArg ? resolve(dataArg) : await libraryCopy(join(WORK, 'runs', 'perf', 'data'))
const port = PERF_PORT
const out = { label, exe, runs: {} }

const stats = (xs) => {
  const s = [...xs].sort((a, b) => a - b)
  const q = (p) => s[Math.min(s.length - 1, Math.floor(p * s.length))]
  return {
    n: s.length,
    median: +q(0.5).toFixed(1),
    p95: +q(0.95).toFixed(1),
    p99: +q(0.99).toFixed(1),
    max: +s.at(-1).toFixed(1),
  }
}

// --- Cold start to a usable grid, five launches ------------------------------
// One unmeasured launch first: the first start of a new binary is scanned by Windows.
await close(await launch({ exe, dataDir, port }))
const starts = []
for (let i = 0; i < 5; i++) {
  const app = await launch({ exe, dataDir, port })
  await usableGrid(app.page)
  starts.push(Date.now() - app.started)
  await close(app)
}
out.runs.coldStartMs = { each: starts, median: [...starts].sort((a, b) => a - b)[2] }

// --- Idle memory: a fresh start left alone on the grid for a minute, no model loaded ---
{
  const idle = await launch({ exe, dataDir, port })
  await usableGrid(idle.page)
  await sleep(60000)
  out.runs.idleMemory = memoryMB(idle.proc.pid)
  await close(idle)
}

// --- One session for the rest -------------------------------------------------
const app = await launch({ exe, dataDir, port })
const { page } = app
await usableGrid(page)
await sleep(3000)

// Frontend boot alone: reloads to a usable grid, and what the page loads.
const boots = []
for (let i = 0; i < 8; i++) {
  await page.reload({ waitUntil: 'domcontentloaded' })
  await usableGrid(page)
  boots.push(
    await page.evaluate(() => {
      const res = performance.getEntriesByType('resource')
      const kb = (ext) =>
        Math.round(res.filter((r) => r.name.endsWith(ext)).reduce((a, r) => a + r.decodedBodySize, 0) / 1024)
      return { usable: Math.round(performance.now()), jsKB: kb('.js'), cssKB: kb('.css') }
    }),
  )
  await sleep(300)
}
out.runs.reloadToUsableMs = {
  each: boots.map((b) => b.usable),
  ...stats(boots.map((b) => b.usable)),
  jsKB: boots[0].jsKB,
  cssKB: boots[0].cssKB,
}

// Scroll: wheel-like steps down the timeline for 10 s, recording frame gaps.
out.runs.scroll = await page.evaluate(async () => {
  const el = document.querySelector('.scroller')
  const gaps = []
  let last = performance.now()
  let run = true
  const frame = (now) => {
    gaps.push(now - last)
    last = now
    if (run) requestAnimationFrame(frame)
  }
  requestAnimationFrame(frame)
  const end = performance.now() + 10000
  while (performance.now() < end) {
    el.scrollTop += 180
    await new Promise((r) => requestAnimationFrame(r))
  }
  run = false
  gaps.shift()
  const s = [...gaps].sort((a, b) => a - b)
  const q = (p) => s[Math.min(s.length - 1, Math.floor(p * s.length))]
  return {
    frames: s.length,
    medianMs: +q(0.5).toFixed(1),
    p95Ms: +q(0.95).toFixed(1),
    p99Ms: +q(0.99).toFixed(1),
    maxMs: +s.at(-1).toFixed(1),
    over33Ms: s.filter((g) => g > 33.4).length,
    scrolledPx: Math.round(el.scrollTop),
  }
})
await page.evaluate(() => (document.querySelector('.scroller').scrollTop = 0))
await sleep(1000)

// Search: the first loads the text model (recorded apart), then five warm.
async function search(q) {
  const input = await page.waitForSelector('.search input')
  await input.focus()
  await page.keyboard.down('Control')
  await page.keyboard.press('a')
  await page.keyboard.up('Control')
  await input.type(q)
  const t = Date.now()
  await page.keyboard.press('Enter')
  await page.waitForFunction(
    (q) =>
      location.pathname === '/search' &&
      new URLSearchParams(location.search).get('q') === q &&
      document.querySelectorAll('.scroller .cell').length > 0,
    { timeout: 120000, polling: 16 },
    q,
  )
  return Date.now() - t
}
try {
  out.runs.firstSearchMs = await search('red circle')
  const warm = []
  for (const q of ['blue square', 'green', 'orange shapes', 'purple circle', 'yellow rectangle'])
    warm.push(await search(q))
  out.runs.warmSearchMs = { each: warm, ...stats(warm) }
} catch (e) {
  out.runs.searchError = String(e)
}

// Viewer: open the first result, step forward ten times, close.
try {
  await page.goto(new URL('/', page.url()).href)
  await usableGrid(page)
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await sleep(300)
  const t = Date.now()
  await page.keyboard.press('Enter')
  const shown = () => {
    const i = document.querySelector('[aria-modal="true"] img')
    return !!i && i.complete && i.naturalWidth > 0
  }
  await page.waitForFunction(shown, { timeout: 15000, polling: 16 })
  out.runs.viewerOpenMs = Date.now() - t
  const steps = []
  for (let i = 0; i < 10; i++) {
    const before = await page.evaluate(
      () => document.querySelector('[aria-modal="true"] img.original, [aria-modal="true"] img')?.currentSrc ?? '',
    )
    const s = Date.now()
    await page.keyboard.press('ArrowRight')
    await page.waitForFunction(
      (before) => {
        const img = document.querySelector('[aria-modal="true"] img.original, [aria-modal="true"] img')
        return img && img.currentSrc !== before && img.complete && img.naturalWidth > 0
      },
      { timeout: 15000, polling: 16 },
      before,
    )
    steps.push(Date.now() - s)
  }
  out.runs.viewerNextMs = { each: steps, ...stats(steps) }
  await page.keyboard.press('Escape')
} catch (e) {
  out.runs.viewerError = String(e)
}

// Memory after use: searches made (the text model loaded), the viewer used.
await sleep(5000)
out.runs.usedMemory = memoryMB(app.proc.pid)
await close(app)

const dir = join(WORK, 'results', 'perf')
mkdirSync(dir, { recursive: true })
writeFileSync(join(dir, `${label}.json`), JSON.stringify(out, null, 2))
console.log(JSON.stringify(out, null, 2))
