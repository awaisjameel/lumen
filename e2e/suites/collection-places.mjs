// Current place metadata across corrections, privacy and collection lifetimes.
import { execFileSync } from 'node:child_process'
import { join } from 'node:path'
import { close, launch, lib, resize, search, unthrottle, waitIndexed } from '../lib/app.mjs'
import { button, dismissNotices, selectPhoto, undoNotice, until } from '../lib/controls.mjs'
import { copyLibrary } from '../lib/fixtures.mjs'
import { bindingID, mutationTrace } from '../lib/mutation-trace.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('collection-places')
const seed = join(s.run, 'seed')
const photos = join(s.run, 'photos')
const fixture = join(E2E, 'fixtures', 'collection_places.py')
const readMethods = new Map([[bindingID('libraryservice', 'TopPlaces'), 'TopPlaces']])
const emptyRule = { kind: '', favourites: false, from: '', to: '', tag: 0, person: 0 }
const PIN = '612435'
const external = []
const diagnostics = []
const consoleReads = []
const pageErrors = []
const observations = []
let app
let trace
let dataDir = seed
let faultActive = false
let phase = 'fixture'
let failure
let first
let second
let origin
let corrected
let manual
let smart
let other
let tag
let collections

function fault(action) {
  execFileSync(process.env.PYTHON ?? 'python', [fixture, action, join(dataDir, 'library.db')], { stdio: 'inherit' })
  faultActive = action === 'refuse'
}
function watch(page) {
  s.watch(page)
  page.on('pageerror', (error) => pageErrors.push({ phase, error: String(error) }))
  page.on('console', (message) => {
    if (message.type() !== 'error') return
    const current = { phase, fault: faultActive }
    consoleReads.push(Promise.all(message.args().map((value) => value.evaluate((value) =>
      value && typeof value.message === 'string' ? value.message : String(value)).catch(() => message.text()))).then((parts) =>
      diagnostics.push({ ...current, error: parts.join(' ') || message.text() })))
  })
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname)) external.push(url.href)
  })
}
async function start(label, baseline) {
  dataDir = copyLibrary(seed, join(s.run, label))
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await waitIndexed(app.page, { ai: true, atLeast: 2, timeout: 120_000, log: console.log })
  await lib(app.page, 'SetPaused', true)
  trace = await mutationTrace(app.page, baseline, readMethods)
  if (baseline) await app.page.reload()
  await resize(app.page, 1440, 1000)
}
async function stop() {
  if (faultActive) fault('restore')
  if (app) await close(app)
  app = undefined
}
async function openCollection(page, collection) {
  await page.locator(`aside a[href="${collection.list}"]`).click()
  await page.locator(`a[href="${collection.path}"]`).click()
  await page.waitForSelector('.collection-hero .meta')
  await until(() => page.evaluate((path) => location.pathname === path, collection.path), 'the collection did not open')
}
async function header(page, city) {
  await until(() => page.$$eval('[data-collection-place]', (links, city) => links.length === 1 &&
    links[0].textContent.trim() === city.name && links[0].getAttribute('href') === `/places/${city.id}`, city),
  `the current header did not link to ${city.name}`)
}
async function correctPlace(page, city) {
  await selectPhoto(page, first)
  await page.locator('[data-edit-place]').click()
  await page.locator('input[aria-label="Latitude"]').fill(String(city.lat))
  await page.locator('input[aria-label="Longitude"]').fill(String(city.lon))
  await button(page, 'Save place', '[role="dialog"]')
  await page.waitForSelector('#correct-place', { hidden: true })
  return undoNotice(page)
}
async function membership(page, collection) {
  const selection = await lib(page, 'Selection', collection.view, [], false)
  return selection.ids.length === 1 && selection.ids[0] === first
}
try {
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', photos, '2'], { stdio: 'inherit' })
  app = await launch({ exe: s.exe, dataDir: seed, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await lib(app.page, 'AddSources', [photos])
  await waitIndexed(app.page, { ai: true, atLeast: 2, timeout: 120_000, log: console.log })
  await lib(app.page, 'SetPaused', true)
  const ids = (await lib(app.page, 'Selection', { filter: '' }, [], false)).ids.sort((a, b) => a - b)
  s.must('two generated photos are indexed', ids.length === 2, ids)
  ;[first, second] = ids
  origin = (await lib(app.page, 'FindCities', 'Karachi', 5))[0]
  corrected = (await lib(app.page, 'FindCities', 'Lahore', 5))[0]
  s.must('the bundled atlas supplies two different cities', origin && corrected && origin.id !== corrected.id, { origin, corrected })
  await lib(app.page, 'SetTakenAt', [first], Date.UTC(2001, 0, 1) / 1000)
  await lib(app.page, 'SetTakenAt', [second], Date.UTC(2010, 0, 1) / 1000)
  await lib(app.page, 'SetLocation', ids, origin.lat, origin.lon)
  await search(app.page, 'TagAsset', first, 'Native locality')
  tag = (await search(app.page, 'Things')).find((thing) => thing.name === 'Native locality').id
  manual = await lib(app.page, 'CreateAlbum', 'Place metadata manual', 'manual', emptyRule, [first])
  smart = await lib(app.page, 'CreateAlbum', 'Place metadata smart', 'smart', { ...emptyRule, tag }, [])
  other = await lib(app.page, 'CreateAlbum', 'Other place album', 'manual', emptyRule, [second])
  await lib(app.page, 'CreateBlockPIN', PIN)
  collections = [
    { name: 'manual album', list: '/albums', path: `/albums/${manual}`, view: { filter: '', album: manual } },
    { name: 'smart album', list: '/albums', path: `/albums/${smart}`, view: { filter: '', album: smart } },
    { name: 'Thing', list: '/things', path: `/things/${tag}`, view: { filter: '', tag } },
    { name: 'event', list: '/events', path: `/events/${first}`, view: { filter: '', event: first } },
  ]
  await stop()

  if (process.env.LUMEN_BASELINE_FRONTEND) {
    phase = 'built parent reproduction'
    await start('baseline', process.env.LUMEN_BASELINE_FRONTEND)
    const page = app.page
    await openCollection(page, collections[0])
    await until(() => page.$eval('.collection-hero .meta', (node, name) => node.textContent.includes(name), origin.name),
      'the parent did not show the original city')
    await correctPlace(page, corrected)
    await until(() => page.$eval('.inspector-body', (node, name) => node.textContent.includes(name), corrected.name),
      'the corrected inspector did not refresh')
    // Give the parent's existing three-second event path its full window.
    const started = await page.evaluate(() => performance.now())
    await page.waitForFunction((started) => performance.now() - started > 3500, {}, started)
    const caption = await page.$eval('.collection-hero .meta', (node) => node.textContent)
    const stored = await lib(page, 'TopPlaces', collections[0].view)
    const observation = { caption, stored, sameMembership: await membership(page, collections[0]) }
    observations.push({ phase, ...observation })
    s.check('the built parent retains the old city after a completed same-count correction',
      caption.includes(origin.name) && !caption.includes(corrected.name) && stored.length === 1 &&
      stored[0].id === corrected.id && observation.sameMembership, observation)
    await page.screenshot({ path: join(s.out, 'parent-stale-place.png') })
    s.check('parent tracing has no errors', trace.errors.length === 0, trace.errors)
    await stop()
  }

  phase = 'candidate correction and Undo'
  await start('candidate')
  const page = app.page
  for (const collection of collections) {
    phase = `${collection.name} correction and Undo`
    await openCollection(page, collection)
    await header(page, origin)
    await trace.idle()
    trace.reset()
    const inverse = await correctPlace(page, corrected)
    await header(page, corrected)
    s.check(`${collection.name}: current city and destination update without changing membership`,
      await membership(page, collection) && (await lib(page, 'Asset', first)).placeId === corrected.id)
    await button(page, 'Undo', `[data-notification="${inverse}"]`)
    await header(page, origin)
    await page.locator('[data-collection-place]').click()
    await until(() => page.evaluate((id) => location.pathname === `/places/${id}`, origin.id), 'the current city link did not navigate')
    await page.goBack()
    await header(page, origin)
    await page.goForward()
    await until(() => page.evaluate((id) => location.pathname === `/places/${id}`, origin.id), 'forward navigation lost the city identity')
    await page.goBack()
    await header(page, origin)
    await trace.idle()
    s.check(`${collection.name}: Undo and back/forward preserve the current metadata and collection`,
      await membership(page, collection) && await page.evaluate((path) => location.pathname === path, collection.path))
    observations.push({ phase, calls: trace.calls.filter((row) => row.name === 'TopPlaces') })
    await dismissNotices(page)
  }

  phase = 'bounded background corrections'
  await openCollection(page, collections[0])
  await header(page, origin)
  await trace.idle()
  trace.reset()
  for (let i = 0; i < 10; i++) {
    const city = i % 2 ? corrected : origin
    await lib(page, 'SetLocation', [first], city.lat, city.lon)
  }
  await header(page, corrected)
  await trace.idle()
  const background = trace.calls.filter((row) => row.name === 'TopPlaces')
  observations.push({ phase, calls: background })
  s.check('ten same-count background corrections share bounded current metadata reads',
    background.length > 0 && background.length <= 2 && await membership(page, collections[0]), background)

  phase = 'privacy clearing and failed-read recovery'
  await selectPhoto(page, first)
  trace.holdNext((row) => row.name === 'TopPlaces')
  await button(page, 'Block this place', '.inspector-body')
  await page.locator('label::-p-text(Hide the name only)').click()
  await page.locator('[data-block="save"]').click()
  await until(() => trace.held, 'the privacy refresh did not reach its current metadata read')
  s.check('blocking clears the old place before its replacement read completes', !(await page.$('[data-collection-place]')))
  fault('refuse')
  await trace.release()
  await page.waitForSelector('[data-collection-places-error]', { visible: true })
  s.check('a failed privacy refresh cannot restore the protected city or link', !(await page.$('[data-collection-place]')))
  await trace.idle()
  const failedReads = trace.calls.filter((row) => row.name === 'TopPlaces' && row.status >= 400)
  s.check('the native backend actually refuses the injected metadata read', failedReads.length > 0, failedReads)
  observations.push({ phase, refused: failedReads })
  await page.screenshot({ path: join(s.out, 'private-place-read-failure.png') })
  fault('restore')
  trace.reset()
  await button(page, 'Try again', '[data-collection-places-error]')
  await page.waitForSelector('[data-collection-places-error]', { hidden: true })
  await trace.idle()
  const covered = await lib(page, 'Asset', first)
  s.check('retry reads the current blocked state without replaying a correction',
    !(await page.$('[data-collection-place]')) && covered.placeId === 0 && covered.lat === corrected.lat &&
    covered.lon === corrected.lon &&
    !trace.calls.some((row) => row.write))
  await button(page, 'Undo', '[data-notification="block.undo"]')
  await header(page, corrected)
  s.check('normal block Undo restores the current city in place',
    await membership(page, collections[0]) && (await lib(page, 'Asset', first)).placeId === corrected.id)
  await dismissNotices(page)

  phase = 'old read after navigation'
  trace.holdNext((row) => row.name === 'TopPlaces')
  await lib(page, 'SetLocation', [first], corrected.lat, corrected.lon)
  await until(() => trace.held, 'the older collection metadata did not reach its read')
  await openCollection(page, { list: '/albums', path: `/albums/${other}` })
  await header(page, origin)
  await trace.release()
  await trace.idle()
  s.check('a late abandoned collection read cannot replace the new header',
    await page.evaluate((id) => location.pathname === `/albums/${id}`, other) &&
    (await page.$$eval('[data-collection-place]', (nodes) => nodes.map((node) => node.getAttribute('href')))).join() === `/places/${origin.id}`)
  await page.screenshot({ path: join(s.out, 'current-place-after-navigation.png') })
  s.check('candidate bridge instrumentation has no errors', trace.errors.length === 0, trace.errors)
  s.check('no external requests', external.length === 0, external)
  await Promise.all(consoleReads)
  // The scratch column fault also refuses the grid's unqualified place-name
  // read. Only these exact injected failures are expected; uncaught promise
  // failures and every other error still fail acceptance.
  const unexpected = diagnostics.filter((row) => !/custom\.js|Failed to load resource/.test(row.error) &&
    !(row.phase === 'privacy clearing and failed-read recovery' && /^no such column: (?:p\.)?name$/.test(row.error)))
  s.check('only the deliberately refused place-name reads are logged', pageErrors.length === 0 && unexpected.length === 0, { pageErrors, unexpected })
} catch (error) {
  failure = { phase, stack: error.stack ?? String(error) }
  s.check(`collection metadata completed (${phase})`, false, String(error))
  if (app) {
    failure.header = await app.page.evaluate(() => document.querySelector('.collection-hero')?.textContent).catch(() => undefined)
    await app.page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
  }
} finally {
  await trace?.release().catch(() => {})
  await stop()
  await Promise.allSettled(consoleReads)
  s.finish({ failure, observations, diagnostics, pageErrors, reads: failure ? trace?.calls.filter((row) => row.name === 'TopPlaces') : undefined })
}
