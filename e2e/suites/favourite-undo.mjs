// The same indexed synthetic library drives both built frontends against
// the candidate's unchanged favourite service/store. No user media is read.
import { execFileSync } from 'node:child_process'
import { join } from 'node:path'
import { close, launch, lib, resize, search, sleep, unthrottle, waitIndexed } from '../lib/app.mjs'
import { copyLibrary } from '../lib/fixtures.mjs'
import { favouriteTrace } from '../lib/favourite-trace.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('favourite-undo')
const photos = join(s.run, 'photos')
const seed = join(s.run, 'seed')
const N = 500
const PIN = '624815'
let app
let trace
const measurements = {}
const external = []
function watch(page) {
  s.watch(page)
  page.on('pageerror', (error) => console.log(`PAGE ERROR ${error.stack ?? error}`))
  page.on('console', (message) => {
    if (message.type() === 'error') console.log(`CONSOLE ERROR ${message.text()} ${JSON.stringify(message.location())}`)
  })
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname))
      external.push(request.url())
  })
}
async function until(test, message) {
  const end = Date.now() + 30_000
  while (Date.now() < end) {
    if (await test()) return
    await sleep(30)
  }
  throw new Error(message)
}
async function button(page, text, root = '') {
  const selector = `${root ? `${root} ` : ''}button::-p-text(${text})`
  await page.waitForSelector(selector, { visible: true })
  const buttons = await page.$$(selector)
  for (const element of buttons)
    if (await element.evaluate((node, text) => node.textContent.trim() === text, text)) {
      await element.click()
      return
    }
  throw new Error(`missing button: ${text}`)
}
async function favouriteAll(page) {
  const clear = await page.$('button[aria-label="Clear selection"]')
  if (clear) await clear.click()
  await page.click('button[aria-label="Selection options"]')
  await page.waitForSelector('[role="menuitem"]::-p-text(Select all results)', { visible: true })
  await page.click('[role="menuitem"]::-p-text(Select all results)')
  await page.waitForSelector('[data-command="library.favouriteSelection"]', { visible: true })
  await page.click('[data-command="library.favouriteSelection"]')
  await until(async () => (await lib(page, 'Counts')).favourites === N, 'batch favourite did not finish')
  await trace.idle()
  await page.waitForFunction(() =>
    [...document.querySelectorAll('button')].some((el) => el.textContent.trim() === 'Undo'),
  )
}
async function dismissNotices(page) {
  for (const close of await page.$$('button[aria-label="Dismiss notification"]')) await close.click()
}
async function startFrames(page) {
  await page.evaluate(() => {
    const sample = { active: true, last: performance.now(), max: 0, frames: 0 }
    window.__favouriteFrames = sample
    function frame(now) {
      if (!sample.active) return
      sample.max = Math.max(sample.max, now - sample.last)
      sample.last = now
      sample.frames++
      requestAnimationFrame(frame)
    }
    requestAnimationFrame(frame)
  })
}
async function frames(page) {
  return page.evaluate(() => {
    const sample = window.__favouriteFrames
    sample.active = false
    return { frames: sample.frames, maxFrameGapMS: Number(sample.max.toFixed(2)) }
  })
}
async function measure(label, baselineDir) {
  app = await launch({ exe: s.exe, dataDir: copyLibrary(seed, join(s.run, label)), port: s.port })
  watch(app.page)
  await lib(app.page, 'SetPaused', true)
  await app.page.waitForSelector('.scroller .cell')
  trace = await favouriteTrace(app.page, baselineDir)
  if (baselineDir) await app.page.reload()
  await resize(app.page, 1440, 900)
  await app.page.waitForSelector('.scroller .cell')
  trace.reset()
  await favouriteAll(app.page)
  const forward = trace.summary()
  trace.reset()
  await startFrames(app.page)
  const started = performance.now()
  await button(app.page, 'Undo', baselineDir ? '' : '[data-notification^="undo."]')
  await until(
    async () => (await lib(app.page, 'Counts')).favourites === 0,
    'favourite Undo did not restore the persisted flags',
  )
  await trace.idle()
  const inverse = {
    ...trace.summary(),
    milliseconds: Number((performance.now() - started).toFixed(2)),
    ...(await frames(app.page)),
  }
  measurements[label] = { forward, inverse }
  console.log(`MEASURE ${label} ${JSON.stringify(measurements[label])}`)
  s.check(
    `${label}: all flags restored through real controls`,
    (await lib(app.page, 'Counts')).favourites === 0,
    measurements[label],
  )
  s.must(`${label}: restoration writes were observed`, inverse.writes > 0, inverse)
  s.check(`${label}: bridge tracing succeeded`, trace.errors.length === 0, trace.errors)
  if (!baselineDir) {
    s.must('candidate: previous reads and writes carry at most 200 IDs', forward.maxIDs <= 200, forward)
    s.must('candidate: one inverse call is outstanding at a time', inverse.maxOutstandingWrites === 1, inverse)
    s.must(
      'candidate: 500 inverses use three bounded batch calls',
      JSON.stringify(inverse.parts) === '[200,200,100]' && inverse.writes === 3,
      inverse,
    )
  }
}
try {
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', photos, String(N)], {
    stdio: 'inherit',
  })
  app = await launch({ exe: s.exe, dataDir: seed, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await lib(app.page, 'AddSources', [photos])
  await waitIndexed(app.page, { ai: true, atLeast: N, timeout: 900_000, log: console.log })
  await lib(app.page, 'SetPaused', true)
  await close(app)
  app = undefined

  if (process.env.LUMEN_FAVOURITE_BASELINE_FRONTEND) {
    await measure('baseline', process.env.LUMEN_FAVOURITE_BASELINE_FRONTEND)
    await close(app)
    app = undefined
  }
  await measure('candidate')
  const { page } = app
  await dismissNotices(page)
  trace.reset()
  await favouriteAll(page)
  const target = trace.calls
    .filter((row) => row.name === 'ApplyBatch')
    .flatMap((row) => row.ids)
    .at(-1)
  s.must('a target outside the first inverse unit is available', Number.isInteger(target), target)
  await lib(page, 'CreateBlockPIN', PIN)
  const block = await lib(page, 'SetBlock', 'asset', target, '', 'photos')
  trace.reset()
  trace.holdUndo()
  await button(page, 'Undo', '[data-notification^="undo."]')
  await until(() => trace.held, 'inverse did not reach its bounded service call')
  await page.click('aside a[href="/things"]')
  await page.waitForFunction(() => location.pathname === '/things')
  s.check(
    'navigation remains usable while an inverse is pending',
    await page.evaluate(() => location.pathname === '/things'),
  )
  await button(page, 'Cancel remaining', '[data-notification^="undo."]')
  await trace.release()
  await until(
    async () => (await lib(page, 'Counts')).favourites === N - 201,
    'the completed inverse unit was not retained',
  )
  await page.waitForFunction(() => document.body.innerText.includes('300 cancelled'))
  s.check('cancellation keeps 200 commits and 300 resumable targets', trace.summary().writes === 1, trace.summary())
  await page.click('aside a[href="/"]')
  await page.waitForSelector('.scroller .cell')
  const clear = await page.$('button[aria-label="Clear selection"]')
  if (clear) await clear.click()
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  const focus = await page.$eval('.scroller', (el) => el.getAttribute('aria-activedescendant'))
  s.must('the new view has its own focused item', !!focus, focus)
  await button(page, 'Resume cancelled items', '[data-notification^="undo."]')
  await until(async () => (await lib(page, 'Counts')).favourites === 0, 'resumed visible inverses did not complete')
  await page.waitForFunction(
    () => document.body.innerText.includes('499 restored') && document.body.innerText.includes('1 failed'),
  )
  s.check(
    'completion retains another view’s current focus',
    (await page.$eval('.scroller', (el) => el.getAttribute('aria-activedescendant'))) === focus,
    focus,
  )
  await resize(page, 800, 800)
  await button(page, 'View outcomes', '[data-notification^="undo."]')
  await page.waitForSelector('[role="dialog"] [data-undo-item]')
  s.check('large outcomes render one page', (await page.$$eval('[data-undo-item]', (rows) => rows.length)) === 100)
  s.check(
    'compact outcomes have no sideways overflow',
    await page.$eval('[role="dialog"]', (el) => el.scrollWidth <= el.clientWidth),
  )
  await page.screenshot({ path: join(s.out, 'partial-undo.png') })
  await page.keyboard.press('Escape')
  await page.waitForFunction(() => !document.querySelector('[role="dialog"]'))
  await lib(page, 'UnlockBlocks', PIN)
  await lib(page, 'RemoveBlock', block.id)
  await lib(page, 'LockBlocks')
  trace.reset()
  await button(page, 'Retry failed items', '[data-notification^="undo."]')
  await page.waitForFunction(() => document.body.innerText.includes('500 restored'))
  await trace.idle()
  const retried = trace.calls.filter((row) => row.write)
  s.check(
    'retry writes only the refused original target',
    retried.length === 1 && JSON.stringify(retried[0]?.ids) === JSON.stringify([target]),
    trace.summary(),
  )

  await dismissNotices(page)
  trace.reset()
  await favouriteAll(page)
  const cell = await page.$eval('.scroller .cell[id]', (el) => el.id)
  const edited = Number(cell.slice('cell-'.length))
  await page.hover(`#${cell}`)
  await page.waitForSelector(`#${cell} button[aria-label="Remove favourite"]`)
  await page.click(`#${cell} button[aria-label="Remove favourite"]`)
  await until(async () => !(await search(page, 'Hits', [edited]))[0]?.f, 'the new unfavourite did not persist')
  await page.waitForSelector(`#${cell} button[aria-label="Favourite"]`)
  await page.click(`#${cell} button[aria-label="Favourite"]`)
  await until(async () => !!(await search(page, 'Hits', [edited]))[0]?.f, 'the later favourite did not persist')
  await trace.idle()
  trace.reset()
  await button(page, 'Undo', '[data-notification^="undo."]')
  await page.waitForFunction(
    () => document.body.innerText.includes('499 restored') && document.body.innerText.includes('newer edits kept: 1'),
  )
  await trace.idle()
  s.check(
    'a later explicit edit survives the older Undo',
    (await lib(page, 'Counts')).favourites === 1 && !trace.calls.some((row) => row.ids.includes(edited)),
    { edited, inverse: trace.summary() },
  )
  await close(app)
  app = undefined
  app = await launch({ exe: s.exe, dataDir: join(s.run, 'candidate'), port: s.port })
  watch(app.page)
  s.check(
    'the final annotation state survives a restart',
    (await lib(app.page, 'Counts')).favourites === 1 && !!(await search(app.page, 'Hits', [edited]))[0]?.f,
  )
  s.check('no external requests', external.length === 0, external)
  s.check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors())
} catch (error) {
  s.check('favourite Undo acceptance ran to the end', false, String(error))
  if (app) await app.page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  await trace?.release().catch(() => {})
  if (app) await close(app)
  s.finish({ measurements })
}
