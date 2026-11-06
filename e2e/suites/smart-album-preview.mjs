// Bounded smart rules through the real editor and the parent/candidate builds.
import { execFileSync } from 'node:child_process'
import { mkdirSync, readFileSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { close, launch, lib, people, resize, search, unthrottle, waitIndexed } from '../lib/app.mjs'
import { button, option as chooseOption, until } from '../lib/controls.mjs'
import { copyLibrary } from '../lib/fixtures.mjs'
import { bindingID, mutationTrace } from '../lib/mutation-trace.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('smart-album-preview')
const seed = join(s.run, 'seed')
const photos = join(s.run, 'photos')
const markerFile = join(photos, 'preview-scan-barrier.png')
const fixtureScript = join(E2E, 'fixtures', 'album_preview.py')
const readMethods = new Map(['Timeline', 'Days', 'PreviewAlbum'].map((name) => [bindingID('libraryservice', name), name]))
const readNames = new Set(readMethods.values())
const rule = (overrides = {}) => ({ kind: '', favourites: false, from: '', to: '', tag: 0, person: 0, ...overrides })
const PIN = '420521'
const external = []
const metrics = {}
let app
let trace
let dataDir = seed
let phase = 'fixture'
let failure
function fixture(action, argument = '') {
  const out = execFileSync(process.env.PYTHON ?? 'python', [fixtureScript, action,
    action === 'marker' ? photos : join(dataDir, 'library.db'), argument], { encoding: 'utf8' })
  return out.trim() ? JSON.parse(out) : undefined
}
function watch(page) {
  s.watch(page, { context: () => phase, expected: row => row.kind === 'response' && row.context === 'blocked and missing references with retry' && row.method === 'PreviewAlbum' && ['album_reference', 'not_found'].includes(row.code) })
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname)) external.push(url.href)
  })
}
async function settled(page, minimum) {
  await waitIndexed(page, { ai: true, atLeast: minimum, timeout: 180_000, log: console.log })
  await until(async () => {
    const p = await lib(page, 'Progress')
    return !p.scanning && p.videos === 0 && !p.working.videos && !p.working.embedding && !p.working.faces
  }, 'the real mixed-media fixture did not settle', 180_000)
  await lib(page, 'SetPaused', true)
}
async function start(label, baselineDir) {
  fixture('marker', label)
  dataDir = copyLibrary(seed, join(s.run, label))
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await settled(app.page, 10) // The new image proves the startup scan finished before metadata injection.
  trace = await mutationTrace(app.page, baselineDir, readMethods)
  if (baselineDir) await app.page.reload()
  await resize(app.page, 1440, 1000)
}
async function stop() {
  if (app) await close(app)
  app = undefined
  rmSync(markerFile, { force: true })
}
async function albums(page) {
  await page.locator('aside a[href="/albums"]').click()
  await page.waitForSelector('[data-action="new-album"]', { visible: true })
}
async function choose(page, label, option) {
  const selector = `#album-editor button[aria-label="${label}"]`
  if ((await page.$eval(selector, (node) => node.innerText.trim())) === option) return
  await page.locator(selector).click()
  await chooseOption(page, option)
}
async function favourites(page, on) {
  const selector = '#album-editor [role="checkbox"]'
  if ((await page.$eval(selector, (node) => node.getAttribute('aria-checked'))) !== String(on)) await page.locator(selector).click()
}
async function editor(page, name) {
  await page.locator('[data-action="new-album"]').click()
  await page.locator('#album-editor input[aria-label="Album name"]').fill(name)
  await choose(page, 'Album type', 'Smart album')
  await page.waitForSelector('[data-album-preview]', { visible: true })
}
async function count(page, expected) {
  const text = `${new Intl.NumberFormat('en').format(expected)} matching item${expected === 1 ? '' : 's'}`
  await until(() => page.$eval('[data-album-preview]', (node, text) => node.innerText.includes(text), text),
    `the editor did not show ${text}`)
  await until(() => page.$$eval('[data-album-preview] .preview-grid > div', (nodes, expected) => nodes.length === Math.min(3, expected), expected),
    'the editor did not render the bounded sample')
}
function statistics(started) {
  const rows = trace.calls.filter((row) => readNames.has(row.name))
  return {
    calls: rows.length,
    days: rows.filter((row) => row.name === 'Days').length,
    previews: rows.filter((row) => row.name === 'PreviewAlbum').length,
    responseBytes: rows.reduce((n, row) => n + (row.responseBytes ?? 0), 0),
    requestBytes: rows.reduce((n, row) => n + row.bytes, 0),
    readMilliseconds: Number(rows.reduce((n, row) => n + (row.milliseconds ?? 0), 0).toFixed(2)),
    totalMilliseconds: Number((performance.now() - started).toFixed(2)),
    responsesComplete: rows.every((row) => row.status === 200 && row.responseBytes > 0 && !row.responseError),
  }
}
async function measure(label, baselineDir) {
  phase = `${label} large-fixture measurement`
  // Later children inherit #42. Their built parent must retain the bounded
  // contract; only a parent whose generated API predates it should amplify.
  const bounded = !baselineDir || readFileSync(join(baselineDir, '..', 'bindings', 'lumen', 'services', 'libraryservice.ts'), 'utf8')
    .includes('export function PreviewAlbum(')
  await start(label, baselineDir)
  const page = app.page
  await lib(page, 'CreateAlbum', 'Preview warm-up', 'smart', rule(), [])
  await albums(page)
  const population = fixture('large')
  await lib(page, 'SetFavourite', 1, false) // Run the store's normal derived-count settlement after fixture setup.
  await lib(page, 'SetTakenAt', [1], 1748736001)
  // A real collection reload proves earlier coalesced library events have
  // settled before measurement; it cannot add a stray preview refresh.
  await page.waitForFunction(() => document.querySelector('.collection-card .count')?.textContent.includes('100,000'))
  const totals = await lib(page, 'Counts')
  s.must(`${label} uses the same 100,000-row distribution`, totals.photos === population.photos && totals.videos === 20_000, totals)
  await editor(page, 'Measured preview')
  await count(page, population.total)
  await trace.idle()
  const result = { population, contract: bounded ? 'bounded' : 'legacy-days', photos: [], favouriteVideos: [] }
  for (let repetition = 0; repetition < 5; repetition++) {
    await choose(page, 'Media', 'Photos and videos')
    await count(page, population.total)
    await trace.idle()
    trace.reset()
    let started = performance.now()
    await choose(page, 'Media', 'Photos')
    await count(page, population.photos)
    await trace.idle()
    result.photos.push(statistics(started))

    await choose(page, 'Media', 'Videos')
    await favourites(page, false)
    await count(page, 20_000)
    await trace.idle()
    trace.reset()
    started = performance.now()
    await favourites(page, true)
    await count(page, population.favouriteVideos)
    await trace.idle()
    result.favouriteVideos.push(statistics(started))
    await favourites(page, false)
    await choose(page, 'Media', 'Photos and videos')
    await count(page, population.total)
    await trace.idle()
  }
  const samples = [...result.photos, ...result.favouriteVideos]
  s.check(`${label} records complete response payload and timing measurements`, samples.every((row) => row.responsesComplete), result)
  if (baselineDir && !bounded) {
    s.check('the built parent reproduces 100 day reads for each final draft', samples.every((row) => row.days === 100 && row.calls === 101), result)
  } else {
    s.check(`every ${label} draft uses one bounded preview call and no timeline/day calls`, samples.every((row) => row.calls === 1 && row.previews === 1 && row.days === 0), result)
    s.check(`every ${label} response is below 1 KiB with three samples`, samples.every((row) => row.responseBytes < 1024), result)
  }
  metrics[label] = result
  s.check(`${label} bridge instrumentation has no errors`, trace.errors.length === 0, trace.errors)
  await stop()
}
async function draft(page, value) {
  await choose(page, 'Media', value.kind === 'photo' ? 'Photos' : value.kind === 'video' ? 'Videos' : 'Photos and videos')
  await favourites(page, value.favourites)
  for (const [label, text] of [['Taken on or after', value.from], ['Taken on or before', value.to]]) {
    const selector = `#album-editor input[aria-label="${label}"]`
    if ((await page.$eval(selector, (node) => node.value)) !== text) await page.locator(selector).fill(text)
  }
  await choose(page, 'Thing', value.tag ? 'Native détail' : 'Any Thing')
  await choose(page, 'Person', value.person ? 'Ána' : 'Anyone')
}
async function sampleIDs(page) {
  return page.$$eval('[data-album-preview] img', (nodes) => nodes.map((node) => Number(/\/thumb\/(\d+)\//.exec(node.src)?.[1])))
}
try {
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', photos, '6'], { stdio: 'inherit' })
  mkdirSync(join(photos, 'videos'), { recursive: true })
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'videos', join(photos, 'videos')], { stdio: 'inherit' })
  app = await launch({ exe: s.exe, dataDir: seed, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await lib(app.page, 'SetVideos', true)
  await lib(app.page, 'AddSources', [photos])
  await settled(app.page, 9)
  await stop()
  if (process.env.LUMEN_BASELINE_FRONTEND) await measure('baseline', process.env.LUMEN_BASELINE_FRONTEND)
  await measure('candidate')

  phase = 'native rule intersections'
  await start('editor')
  const page = app.page
  const ids = (await lib(page, 'Selection', { filter: '' }, [], false)).ids.sort((a, b) => a - b)
  s.must('the semantic fixture has ten real mixed-media assets', ids.length === 10, ids)
  const rows = []
  for (let i = 0; i < ids.length; i++) {
    const asset = await lib(page, 'Asset', ids[i])
    const day = `2025-03-${14 + i % 3}`
    const at = Date.parse(`${day}T${i % 3 === 0 ? '23:59:59' : '00:00:00'}Z`) / 1000
    await lib(page, 'SetTakenAt', [ids[i]], at)
    await lib(page, 'SetFavourite', ids[i], i % 2 === 0)
    if (i % 4 < 2) await search(page, 'TagAsset', ids[i], 'Native détail')
    rows.push({ id: ids[i], kind: asset.kind, day, at, favourite: i % 2 === 0, tagged: i % 4 < 2, person: i % 3 !== 0, shown: i !== ids.length - 1 })
  }
  const faces = fixture('faces', JSON.stringify(rows.filter((row) => row.person).map((row) => row.id)))
  const person = await people(page, 'NameFaces', faces, 'Ána')
  const tag = (await search(page, 'Things')).find((item) => item.name === 'Native détail').id
  await lib(page, 'CreateBlockPIN', PIN)
  await lib(page, 'SetBlock', 'asset', ids.at(-1), '', 'photos')
  await page.waitForFunction(() => document.querySelector('[data-index-status]')?.textContent.includes('9 items'))
  const expected = (value) => rows.filter((row) => row.shown && (!value.kind || value.kind === row.kind) &&
    (!value.favourites || row.favourite) && (!value.from || row.day >= value.from) && (!value.to || row.day <= value.to) &&
    (!value.tag || row.tagged) && (!value.person || row.person)).sort((a, b) => b.at - a.at || b.id - a.id).map((row) => row.id)
  const oracle = await lib(page, 'CreateAlbum', 'Independent saved oracle', 'smart', rule(), [])
  await albums(page)
  await editor(page, 'Current native draft')
  for (const [label, value] of [
    ['all visible media', rule()],
    ['photos', rule({ kind: 'photo' })],
    ['favourite photos', rule({ kind: 'photo', favourites: true })],
    ['favourite videos', rule({ kind: 'video', favourites: true })],
    ['inclusive end-of-day bounds', rule({ from: '2025-03-14', to: '2025-03-14' })],
    ['inclusive midnight bounds', rule({ from: '2025-03-15', to: '2025-03-15' })],
    ['Unicode Thing/person conjunction', rule({ tag, person })],
  ]) {
    await draft(page, value)
    const want = expected(value)
    await count(page, want.length)
    await trace.idle()
    const preview = await lib(page, 'PreviewAlbum', value)
    await lib(page, 'UpdateAlbum', oracle, 'Independent saved oracle', value)
    const saved = (await lib(page, 'Selection', { album: oracle }, [], false)).ids
    s.check(`${label}: editor, bounded API and saved membership agree independently`,
      preview.count === want.length && JSON.stringify(preview.items.map((item) => item.id)) === JSON.stringify(want.slice(0, 3)) &&
      JSON.stringify([...saved].sort((a, b) => a - b)) === JSON.stringify([...want].sort((a, b) => a - b)) &&
      JSON.stringify(await sampleIDs(page)) === JSON.stringify(want.slice(0, 3)), { want, saved, preview })
  }

  phase = 'superseded and cancelled native reads'
  await draft(page, rule({ kind: 'photo' }))
  await count(page, expected(rule({ kind: 'photo' })).length)
  await trace.idle()
  trace.holdNext((row) => row.name === 'PreviewAlbum' && row.parameters[0].kind === 'video')
  await choose(page, 'Media', 'Videos')
  await until(() => trace.held, 'the older draft did not reach its read')
  await choose(page, 'Media', 'Photos')
  await count(page, expected(rule({ kind: 'photo' })).length)
  await trace.release()
  await trace.idle()
  s.check('a late older draft never replaces the newer photo preview',
    JSON.stringify(await sampleIDs(page)) === JSON.stringify(expected(rule({ kind: 'photo' })).slice(0, 3)))
  trace.holdNext((row) => row.name === 'PreviewAlbum')
  await favourites(page, true)
  await until(() => trace.held, 'the closing editor did not reach its pending read')
  await button(page, 'Cancel', '[role="dialog"]')
  await page.waitForSelector('#album-editor', { hidden: true })
  await trace.release()
  await trace.idle()
  s.check('closing the editor removes previews and drops the pending result', !(await page.$('[data-album-preview]')))

  phase = 'blocked and missing references with retry'
  await editor(page, 'Reference recovery draft')
  await draft(page, rule({ tag }))
  await count(page, expected(rule({ tag })).length)
  await trace.idle()
  trace.holdNext((row) => row.name === 'PreviewAlbum')
  await favourites(page, true)
  await until(() => trace.held, 'the reference read did not reach its visibility check')
  const block = await lib(page, 'SetBlock', 'tag', tag, '', 'label')
  await trace.release()
  await page.waitForSelector('[data-album-reference-error]', { visible: true })
  s.check('a newly blocked reference retires its cached name and sample before a global refresh',
    (await sampleIDs(page)).length === 0 &&
    !(await page.$eval('#album-editor', node => node.textContent.includes('Native détail'))) &&
    await page.$eval('button[form="album-editor"]', node => node.disabled))
  await lib(page, 'UnlockBlocks', PIN)
  await lib(page, 'RemoveBlock', block.id)
  await lib(page, 'LockBlocks')
  await button(page, 'Try again', '[data-album-reference-error]')
  await until(() => page.$eval('[data-action="retry-album-references"]', node => !node.disabled),
    'the current reference choices did not reload')
  await choose(page, 'Thing', 'Native détail')
  await count(page, expected(rule({ tag, favourites: true })).length)
  s.check('read-only retry and an explicit restored choice preserve the unsaved name',
    (await page.$eval('#album-editor input[aria-label="Album name"]', (node) => node.value)) === 'Reference recovery draft')
  await lib(page, 'UpdateAlbum', oracle, 'Independent saved oracle', rule())
  await search(page, 'DeleteTag', tag)
  await favourites(page, false)
  await page.waitForSelector('[data-album-reference-error]', { visible: true })
  s.check('a deleted reference retires its cached name, count and sample',
    (await sampleIDs(page)).length === 0 &&
    !(await page.$eval('#album-editor', node => node.textContent.includes('Native détail'))))
  await choose(page, 'Thing', 'Any Thing')
  await count(page, expected(rule()).length)
  s.check('choosing a valid reference recovers the same draft',
    (await page.$eval('#album-editor input[aria-label="Album name"]', (node) => node.value)) === 'Reference recovery draft')
  await page.screenshot({ path: join(s.out, 'native-rule-preview.png') })
  s.check('bridge observation completed without errors', trace.errors.length === 0, trace.errors)
  s.check('no external requests', external.length === 0, external)
  await s.settleErrors()
  s.check('no unexpected frontend errors', s.appErrors().length === 0, s.appErrors())
} catch (error) {
  failure = { phase, stack: error.stack ?? String(error) }
  if (app) failure.editor = await app.page.evaluate(() => document.querySelector('#album-editor')?.innerText).catch(() => undefined)
  s.check(`smart-album preview completed (${phase})`, false, String(error))
  if (app) await app.page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  await trace?.release().catch(() => {})
  await stop()
  s.finish({ metrics, failure, reads: failure ? trace?.calls.filter((row) => readNames.has(row.name)).slice(-110) : undefined })
}
