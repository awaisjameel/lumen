// Counts thumbnail revalidation in the native WebView without changing cache policy.
// Run from e2e: node perf/thumbs.mjs [photos=3600]. Each run keeps its own library.
import { execFile, execFileSync, spawn } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:net'
import { cpus, release } from 'node:os'
import { isAbsolute, join, relative, resolve } from 'node:path'
import { promisify } from 'node:util'
import puppeteer from 'puppeteer-core'
import { go, lib, sleep, unthrottle, usableGrid, waitIndexed } from '../lib/app.mjs'
import { E2E, EXE, ROOT } from '../lib/suite.mjs'
import { finaliseRequest, quantile } from './thumbs-metrics.mjs'
import { sampleWebviewMemory } from './thumbs-memory.mjs'
import { checkBlockedThumbnail } from './thumbs-privacy.mjs'

const runFile = promisify(execFile)
const photos = Number(process.argv[2] ?? 3600)
if (!Number.isInteger(photos) || photos < 3000) throw new Error('at least 3000 photos are needed')
const run = join(E2E, '.work', 'thumbs-measure', new Date().toISOString().replaceAll(/[:.]/g, '-'))
const dataDir = join(run, 'data')
const fixtures = process.env.LUMEN_THUMBS_FIXTURES ? resolve(process.env.LUMEN_THUMBS_FIXTURES) : run
const fixtureRelative = relative(join(E2E, '.work', 'thumbs-measure'), fixtures)
if (
  isAbsolute(fixtureRelative) ||
  fixtureRelative.startsWith('..') ||
  resolve(fixtures) === resolve(join(E2E, '.work', 'thumbs-measure'))
)
  throw new Error('fixtures must name a previous run under e2e/.work/thumbs-measure')
const corpus = join(fixtures, 'photos')
const extra = join(fixtures, 'indexing-photos')
mkdirSync(corpus, { recursive: true })
mkdirSync(extra, { recursive: true })
const out = {
  exe: EXE,
  run,
  fixtures,
  photos,
  machine: { cpu: cpus()[0]?.model, logicalProcessors: cpus().length, windows: release(), node: process.version },
  executableSHA256: createHash('sha256').update(readFileSync(EXE)).digest('hex'),
  measurements: [],
  webviewMemory: [],
  method: {
    viewport: '1440 x 900 CSS pixels, device scale 1',
    tiles: 'unique grid thumbnail URLs intersecting the scroller, sampled each animation frame',
    noRequest: 'decoded visible URLs with no Network.requestWillBeSent for that URL anywhere in the scenario',
    requests:
      'all thumb/sprite CDP events, including prefetch and cache hits; backend requests exclude unvalidated cache hits',
    status: 'responseReceivedExtraInfo.statusCode when present, otherwise responseReceived.status',
    size: 'loadingFinished.encodedDataLength and dataReceived.dataLength (bytes); cache and 304 values are protocol-reported',
    timing: 'requestWillBeSent to loadingFinished/Failed; summed durations overlap and are not wall time or engine CPU',
    frames:
      'requestAnimationFrame gaps >50 ms; Tracing FireAnimationFrame gaps and long RunTask events cross-check these',
    cache:
      'requestServedFromCache means memory unless fromDiskCache; a raw 304 is network revalidation despite a cached 200 response',
    webviewMemory:
      'sum of Windows PrivateMemorySize64 and WorkingSet64 for msedgewebview2.exe descendants of this scratch Lumen PID; working sets can count shared pages more than once',
  },
}
const save = () => writeFileSync(join(run, 'report.json'), JSON.stringify(out, null, 2) + '\n')
let proc
let browser
let page
let cdp
let active = 'setup'
const requests = new Map()
const traces = []
const mediaURL = (url) => /\/media\/(thumb|sprite)\//.test(url)

async function start() {
  const server = createServer()
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolve)
  })
  const port = server.address().port
  await new Promise((resolve) => server.close(resolve))
  proc = spawn(EXE, [], {
    env: { ...process.env, LUMEN_DATA_DIR: dataDir, LUMEN_DEVTOOLS_PORT: String(port), LUMEN_AI_ON_BATTERY: '1' },
    stdio: 'ignore',
  })
  proc.on('error', (error) => {
    out.launchError = String(error)
  })
  for (let i = 0; i < 300; i++) {
    if (proc.exitCode !== null || out.launchError) throw new Error(out.launchError ?? `Lumen exited ${proc.exitCode}`)
    const targets = await fetch(`http://127.0.0.1:${port}/json/list`)
      .then((r) => r.json())
      .catch(() => [])
    if (targets.some((t) => t.type === 'page' && t.url.startsWith('http'))) {
      browser = await puppeteer.connect({ browserURL: `http://127.0.0.1:${port}`, defaultViewport: null })
      page = (await browser.pages()).find((p) => p.url().startsWith('http'))
      if (page) break
    }
    await sleep(100)
  }
  if (!page) throw new Error('the WebView did not open its app page')
  cdp = await page.createCDPSession()
  out.webview = await cdp.send('Browser.getVersion')
  cdp.on('Network.requestWillBeSent', (e) => {
    if (!mediaURL(e.request.url)) return
    requests.set(e.requestId, {
      scenario: active,
      id: e.requestId,
      url: e.request.url,
      start: e.timestamp,
      requestHeaders: e.request.headers,
      initiator: e.initiator.type,
      memoryEvent: false,
      disk: false,
      bytes: 0,
    })
  })
  cdp.on('Network.requestWillBeSentExtraInfo', (e) => {
    const r = requests.get(e.requestId)
    if (r) r.actualRequestHeaders = e.headers
  })
  cdp.on('Network.requestServedFromCache', (e) => {
    const r = requests.get(e.requestId)
    if (r) r.memoryEvent = true
  })
  cdp.on('Network.responseReceived', (e) => {
    const r = requests.get(e.requestId)
    if (r)
      Object.assign(r, {
        responseStatus: e.response.status,
        disk: !!e.response.fromDiskCache,
        responseHeaders: e.response.headers,
        timing: e.response.timing,
        responseAt: e.timestamp,
      })
  })
  cdp.on('Network.responseReceivedExtraInfo', (e) => {
    const r = requests.get(e.requestId)
    if (r) Object.assign(r, { wireStatus: e.statusCode, wireHeaders: e.headers })
  })
  cdp.on('Network.dataReceived', (e) => {
    const r = requests.get(e.requestId)
    if (r) r.bytes += e.dataLength
  })
  cdp.on('Network.loadingFinished', (e) => {
    const r = requests.get(e.requestId)
    if (r) Object.assign(r, { end: e.timestamp, encodedBytes: e.encodedDataLength })
  })
  cdp.on('Network.loadingFailed', (e) => {
    const r = requests.get(e.requestId)
    if (r) Object.assign(r, { end: e.timestamp, error: e.errorText, cancelled: e.canceled })
  })
  cdp.on('Tracing.dataCollected', (e) => traces.push(...e.value))
  await cdp.send('Network.enable')
  await cdp.send('Performance.enable')
  await cdp.send('Emulation.setDeviceMetricsOverride', {
    width: 1440,
    height: 900,
    deviceScaleFactor: 1,
    mobile: false,
  })
  // The observer survives router changes, but never forces a lazy image to load.
  await page.evaluateOnNewDocument(installObserver)
  await page.evaluate(installObserver)
}

function installObserver() {
  const state = { recording: false, gaps: [], shown: {}, decoded: {}, samples: [], last: 0 }
  window.thumbMeasurement = state
  function frame(now) {
    if (state.recording) {
      if (state.last) state.gaps.push(now - state.last)
      const scroller = document.querySelector('.scroller')
      const bounds = scroller?.getBoundingClientRect()
      const urls = []
      if (bounds) {
        for (const img of scroller.querySelectorAll('.cell img[src*="/media/thumb/"]')) {
          const rect = img.getBoundingClientRect()
          if (
            rect.bottom <= bounds.top ||
            rect.top >= bounds.bottom ||
            rect.right <= bounds.left ||
            rect.left >= bounds.right
          )
            continue
          const url = img.src
          state.shown[url] = true
          if (img.complete && img.naturalWidth) state.decoded[url] = true
          urls.push(url)
        }
      }
      const prior = state.samples.at(-1)
      if (!prior || prior.top !== scroller?.scrollTop || prior.urls.join() !== urls.join())
        state.samples.push({ at: now, top: scroller?.scrollTop, urls })
      state.last = now
    } else state.last = 0
    requestAnimationFrame(frame)
  }
  requestAnimationFrame(frame)
}

async function settle() {
  await sleep(350)
  await page.waitForFunction(
    () => {
      const scroller = document.querySelector('.scroller')
      if (!scroller) return false
      const b = scroller.getBoundingClientRect()
      const cells = [...scroller.querySelectorAll('.cell')].filter((cell) => {
        const r = cell.getBoundingClientRect()
        return r.bottom > b.top && r.top < b.bottom
      })
      return (
        cells.length > 0 &&
        cells.every((cell) => {
          const imgs = [...cell.querySelectorAll('img')]
          return imgs.length && imgs.every((img) => img.complete && img.naturalWidth > 0)
        })
      )
    },
    { timeout: 30000 },
  )
  for (let i = 0; i < 100; i++) {
    if (![...requests.values()].some((r) => r.scenario === active && !r.end)) return
    await sleep(50)
  }
  throw new Error('thumbnail requests did not settle')
}

async function scroll(screens, direction = 1, fast = false) {
  return page.evaluate(
    async (screens, direction, fast) => {
      const el = document.querySelector('.scroller')
      if (!el) throw new Error('no grid')
      const before = el.scrollTop
      const height = el.clientHeight
      if (el.scrollHeight - el.clientHeight < height * 50) throw new Error('the grid is fewer than fifty screens tall')
      for (let i = 0; i < screens; i++) {
        el.scrollTop += direction * height
        await new Promise((resolve) => requestAnimationFrame(resolve))
        if (!fast) await new Promise((resolve) => setTimeout(resolve, 500))
      }
      return { before, after: el.scrollTop, height, screens, direction, fast }
    },
    screens,
    direction,
    fast,
  )
}

async function measure(name, action) {
  console.log(`measuring ${name}`)
  active = name
  traces.length = 0
  const cpuBefore = await cdp.send('Performance.getMetrics')
  await cdp.send('Tracing.start', {
    categories: 'devtools.timeline,disabled-by-default-devtools.timeline.frame',
    transferMode: 'ReportEvents',
  })
  await page.evaluate(() =>
    Object.assign(window.thumbMeasurement, { recording: true, gaps: [], shown: {}, decoded: {}, samples: [], last: 0 }),
  )
  const start = Date.now()
  const detail = await action()
  await settle()
  const observed = await page.evaluate(() => {
    window.thumbMeasurement.recording = false
    return window.thumbMeasurement
  })
  const finished = new Promise((resolve) => cdp.once('Tracing.tracingComplete', resolve))
  await cdp.send('Tracing.end')
  await finished
  const cpuAfter = await cdp.send('Performance.getMetrics')
  const rows = [...requests.values()].filter((r) => r.scenario === name).map(finaliseRequest)
  const urls = new Set(rows.map((r) => r.url))
  const backendURLs = new Set(rows.filter((r) => r.source.startsWith('network')).map((r) => r.url))
  const times = rows.flatMap((r) => (r.durationMs === null ? [] : [r.durationMs]))
  const traceFrames = traces.filter((e) => e.name === 'FireAnimationFrame' && e.ph === 'X')
  const groups = new Map()
  for (const e of traceFrames) {
    const key = `${e.pid}:${e.tid}`
    if (!groups.has(key)) groups.set(key, [])
    groups.get(key).push(e.ts)
  }
  const traceGaps = [...groups.values()].flatMap((ts) =>
    ts
      .sort((a, b) => a - b)
      .slice(1)
      .map((t, i) => (t - ts[i]) / 1000),
  )
  const metric = (name, result) => result.metrics.find((m) => m.name === name)?.value ?? 0
  const result = {
    name,
    wallMs: Date.now() - start,
    detail,
    tilesShown: Object.keys(observed.shown).length,
    tilesDecoded: Object.keys(observed.decoded).length,
    decodedWithoutRequest: Object.keys(observed.decoded).filter((url) => !urls.has(url)).length,
    decodedWithoutBackendRequest: Object.keys(observed.decoded).filter((url) => !backendURLs.has(url)).length,
    requests: rows.length,
    thumbRequests: rows.filter((r) => r.url.includes('/media/thumb/')).length,
    spriteRequests: rows.filter((r) => r.url.includes('/media/sprite/')).length,
    backendRequests: rows.filter((r) => r.source.startsWith('network')).length,
    status200: rows.filter((r) => r.status === 200).length,
    status304: rows.filter((r) => r.status === 304).length,
    errors: rows.filter((r) => r.error || r.status >= 400).length,
    memory: rows.filter((r) => r.source === 'memory-cache').length,
    disk: rows.filter((r) => r.source === 'disk-cache').length,
    medianMs: quantile(times, 0.5),
    p95Ms: quantile(times, 0.95),
    totalRequestMs: times.reduce((a, b) => a + b, 0),
    encodedBytes: rows.reduce((a, r) => a + (r.encodedBytes ?? 0), 0),
    bodyBytes: rows.reduce((a, r) => a + r.bytes, 0),
    frames: observed.gaps.length,
    framesOver50Ms: observed.gaps.filter((g) => g > 50).length,
    frameP95Ms: quantile(observed.gaps, 0.95),
    frameMaxMs: quantile(observed.gaps, 1),
    traceCallbacks: traceFrames.length,
    traceGapsOver50Ms: traceGaps.filter((g) => g > 50).length,
    traceTasksOver50Ms: traces.filter((e) => e.name === 'RunTask' && e.dur > 50000).length,
    taskMs: 1000 * (metric('TaskDuration', cpuAfter) - metric('TaskDuration', cpuBefore)),
    samples: observed.samples,
    rows,
  }
  writeFileSync(join(run, `${name}-trace.json`), JSON.stringify({ traceEvents: traces }))
  out.measurements.push(result)
  save()
  console.log(JSON.stringify({ ...result, samples: undefined, rows: undefined }))
  active = 'between'
}

async function top() {
  await page.evaluate(() => {
    document.querySelector('.scroller').scrollTop = 0
  })
  await settle()
}

async function memory(label) {
  out.webviewMemory.push({
    label,
    at: new Date().toISOString(),
    ...(await sampleWebviewMemory(proc.pid)),
  })
  save()
}

async function privacy() {
  // Let the last indexing notifications settle, then show the target afresh.
  // Three screens take a tile past the grid's overscan, so it is unmounted.
  await sleep(1500)
  await top()
  await scroll(3, 1, true)
  await settle()
  await top()
  const url = await page.$eval('.scroller .cell img', (img) => img.src)
  const id = Number(/\/thumb\/(\d+)\//.exec(url)[1])
  const fetched = await page.evaluate(async (url) => {
    const response = await fetch(url)
    const bytes = new Uint8Array(await response.arrayBuffer())
    window.retainedThumbnail = new Blob([bytes], { type: 'image/jpeg' })
    const img = new Image()
    await new Promise((resolve, reject) => {
      img.onload = resolve
      img.onerror = reject
      img.src = url
    })
    window.retainedThumbnailImage = img
    return { status: response.status, headers: Object.fromEntries(response.headers), bytes: Array.from(bytes) }
  }, url)
  const bytes = Buffer.from(fetched.bytes)
  writeFileSync(join(run, 'privacy-thumbnail.jpg'), bytes)
  // Use the asset's real source path rather than guessing its date folder.
  const detail = await lib(page, 'Asset', id)
  const path = detail.paths[0]
  const folder = path.slice(0, Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')))
  // Remove the tile before blocking, so returning exercises any retained grid image.
  await scroll(3, 1, true)
  await settle()
  await page.waitForFunction(
    (url) => ![...document.querySelectorAll('.scroller .cell img')].some((img) => img.src === url),
    {},
    url,
  )
  out.privacySetup = { url, id }
  save()
  if (!(await lib(page, 'BlockPINConfigured'))) await lib(page, 'CreateBlockPIN', '246810')
  const block = await lib(page, 'SetBlock', 'folder', 0, folder, 'photos')
  await lib(page, 'LockBlocks')
  const after = await page.evaluate(async (url) => {
    const testImage = (src) =>
      new Promise((resolve) => {
        const img = new Image()
        img.onload = () => resolve({ loaded: true, width: img.naturalWidth })
        img.onerror = () => resolve({ loaded: false, width: img.naturalWidth })
        img.src = src
      })
    // Cache-only reads run before ordinary requests can replace the cached entry with a refusal.
    const forced = await fetch(url, { cache: 'force-cache' })
    const forcedBytes = new Uint8Array(await forced.arrayBuffer())
    const only = await fetch(url, { cache: 'only-if-cached', mode: 'same-origin' })
    const onlyBytes = (await only.arrayBuffer()).byteLength
    const image = await testImage(url)
    const r = await fetch(url)
    const normal = { status: r.status, bytes: (await r.arrayBuffer()).byteLength }
    const blobURL = URL.createObjectURL(window.retainedThumbnail)
    const retainedBlob = await testImage(blobURL)
    URL.revokeObjectURL(blobURL)
    const retainedImage = {
      complete: window.retainedThumbnailImage.complete,
      width: window.retainedThumbnailImage.naturalWidth,
    }
    const canvas = document.createElement('canvas')
    canvas.width = window.retainedThumbnailImage.naturalWidth
    canvas.height = window.retainedThumbnailImage.naturalHeight
    const context = canvas.getContext('2d')
    context.drawImage(window.retainedThumbnailImage, 0, 0)
    const retainedPixels = {
      width: canvas.width,
      height: canvas.height,
      firstPixel: Array.from(context.getImageData(0, 0, 1, 1).data),
    }
    return {
      normal,
      image,
      forceCache: { status: forced.status, bytes: forcedBytes.length, data: Array.from(forcedBytes) },
      onlyIfCached: { status: only.status, bytes: onlyBytes },
      retainedBlob,
      retainedImage,
      retainedPixels,
    }
  }, url)
  const forcedHash = createHash('sha256').update(Buffer.from(after.forceCache.data)).digest('hex')
  delete after.forceCache.data
  out.privacy = {
    url,
    id,
    folder,
    block,
    before: {
      status: fetched.status,
      headers: fetched.headers,
      bytes: bytes.length,
      sha256: createHash('sha256').update(bytes).digest('hex'),
    },
    after,
    forcedHash,
  }
  await go(page, '/people')
  await go(page, '/')
  await top()
  out.privacy.gridAfterReturn = await page.evaluate(() => ({
    decodedIDs: [...document.querySelectorAll('.scroller .cell img')]
      .filter((img) => img.complete && img.naturalWidth)
      .map((img) => Number(/\/thumb\/(\d+)\//.exec(img.src)?.[1]))
      .filter(Number.isFinite),
  }))
  save()
  checkBlockedThumbnail(out.privacy)
  return block
}

function cacheEvidence() {
  const bytes = readFileSync(join(run, 'privacy-thumbnail.jpg'))
  const url = Buffer.from(out.privacy.url)
  const needle = bytes.subarray(0, Math.min(128, bytes.length))
  const hits = []
  const errors = []
  let scannedFiles = 0
  let scannedBytes = 0
  function visit(dir) {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name)
      if (entry.isDirectory()) visit(path)
      else if (/Cache[\\/]/.test(path)) {
        try {
          const data = readFileSync(path)
          scannedFiles++
          scannedBytes += data.length
          const urlOffset = data.indexOf(url)
          const prefixOffset = data.indexOf(needle)
          const fullOffset = data.indexOf(bytes)
          if (urlOffset >= 0 || prefixOffset >= 0)
            hits.push({ path, fileBytes: data.length, urlOffset, prefixOffset, fullOffset })
        } catch (error) {
          errors.push({ path, error: String(error) })
        }
      }
    }
  }
  visit(join(dataDir, 'webview'))
  out.privacy.cacheFiles = {
    hits,
    errors,
    scannedFiles,
    scannedBytes,
    method: 'after graceful shutdown: exact URL, JPEG prefix and full JPEG byte search in scratch WebView cache files',
  }
}

async function stop() {
  if (browser) await browser.disconnect()
  if (proc && proc.exitCode === null) {
    execFileSync('taskkill', ['/PID', String(proc.pid)], { stdio: 'pipe' })
    for (let i = 0; i < 300 && proc.exitCode === null; i++) await sleep(100)
    if (proc.exitCode === null) throw new Error(`process ${proc.pid} did not close gracefully`)
  }
  out.closed = { pid: proc?.pid, exitCode: proc?.exitCode }
}

try {
  console.log(`scratch run: ${run}`)
  if (!process.env.LUMEN_THUMBS_FIXTURES)
    await runFile(
      process.env.PYTHON ?? 'python',
      [join(E2E, 'fixtures', 'build.py'), 'library', corpus, String(photos)],
      { cwd: ROOT },
    )
  // A different range keeps the second import distinct from the indexed library.
  if (!process.env.LUMEN_THUMBS_FIXTURES)
    await runFile(
      process.env.PYTHON ?? 'python',
      [
        '-c',
        'import importlib.util,sys,random; from pathlib import Path; from datetime import datetime; s=importlib.util.spec_from_file_location("fixture",sys.argv[1]); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); r=random.Random(19); out=Path(sys.argv[2]); [(m.picture(r,n+100000).save(out/f"IMG_20260101_{n:05d}.jpg",quality=86)) for n in range(1800)]',
        join(E2E, 'fixtures', 'build.py'),
        extra,
      ],
      { cwd: ROOT },
    )
  await start()
  await unthrottle(page)
  await lib(page, 'SetPauseFullScreen', false)
  await lib(page, 'SetAIOnBattery', true)
  await lib(page, 'AddSources', [corpus])
  await page.reload()
  out.idle = await waitIndexed(page, { ai: true, atLeast: photos, log: console.log })
  if (out.idle.errors) throw new Error(`indexing had ${out.idle.errors} errors`)
  await go(page, '/people')
  // Clearing the HTTP cache before entry makes first-load distinct from warm returns.
  await cdp.send('Network.clearBrowserCache')
  await measure('first-load', async () => {
    await go(page, '/', 0)
    await usableGrid(page)
    return page.evaluate(() => {
      const grid = document.querySelector('.scroller')
      return {
        viewportWidth: innerWidth,
        viewportHeight: innerHeight,
        gridHeight: grid.clientHeight,
        scrollHeight: grid.scrollHeight,
        screens: grid.scrollHeight / grid.clientHeight,
      }
    })
  })
  await memory('before-long-scroll')
  await measure('down-ten', async () => {
    const steps = []
    for (let i = 0; i < 10; i++) {
      steps.push(await scroll(1))
      await settle()
    }
    return steps
  })
  await measure('up-ten', async () => {
    const steps = []
    for (let i = 0; i < 10; i++) {
      steps.push(await scroll(1, -1))
      await settle()
    }
    return steps
  })
  await measure('fling-fifty', async () => {
    const down = await scroll(50, 1, true)
    await settle()
    const up = await scroll(50, -1, true)
    return { down, up }
  })
  await memory('after-long-scroll')
  await sleep(30000)
  await memory('at-rest-after-long-scroll')
  active = 'fling-control'
  out.flingControl = await page.evaluate(async () => {
    const el = document.querySelector('.scroller')
    const gaps = []
    const positions = []
    let last = await new Promise((resolve) => requestAnimationFrame(resolve))
    const start = performance.now()
    for (const direction of [1, -1]) {
      for (let i = 0; i < 50; i++) {
        el.scrollTop += direction * el.clientHeight
        const now = await new Promise((resolve) => requestAnimationFrame(resolve))
        gaps.push(now - last)
        positions.push(el.scrollTop)
        last = now
      }
    }
    return {
      wallMs: performance.now() - start,
      gaps,
      positions,
      method: 'same fifty-screen fling without tracing or per-frame tile sampling; Network remains enabled',
    }
  })
  await settle()
  save()
  await measure('leave-return', async () => {
    await go(page, '/people')
    await go(page, '/')
    await usableGrid(page)
  })
  await measure('viewer', async () => {
    await page.focus('.scroller')
    await page.keyboard.press('ArrowRight')
    await page.keyboard.press('Enter')
    await page.waitForSelector('[aria-modal="true"]')
    await page.waitForFunction(() =>
      [...document.querySelectorAll('[aria-modal="true"] img')].some((img) => img.complete && img.naturalWidth),
    )
    await page.keyboard.press('Escape')
    await page.waitForSelector('[aria-modal="true"]', { hidden: true })
  })
  await top()
  await measure('indexing-scroll', async () => {
    await lib(page, 'AddSources', [extra])
    const samples = []
    for (const direction of [1, -1]) {
      for (let i = 0; i < 10; i++) {
        samples.push(await lib(page, 'Progress'))
        await scroll(1, direction)
        await settle()
      }
    }
    return { addedPhotos: 1800, progress: samples }
  })
  out.secondIdle = await waitIndexed(page, { ai: true, atLeast: photos + 1800, log: console.log })
  await top()
  active = 'privacy'
  await privacy()
  await go(page, '/')
  await top()
  await measure('folder-block-scroll', async () => {
    const steps = []
    for (const direction of [1, -1])
      for (let i = 0; i < 10; i++) {
        steps.push(await scroll(1, direction))
        await settle()
      }
    return steps
  })
} catch (error) {
  out.error = String(error.stack ?? error)
  process.exitCode = 1
  console.error(out.error)
} finally {
  try {
    await stop()
  } catch (error) {
    out.shutdownError = String(error)
    process.exitCode = 1
  }
  if (out.privacy && !out.shutdownError) {
    try {
      cacheEvidence()
    } catch (error) {
      out.cacheEvidenceError = String(error)
      process.exitCode = 1
    }
  }
  out.allRequests = [...requests.values()]
  save()
  console.log(`report: ${join(run, 'report.json')}`)
}
