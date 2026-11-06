// Partial membership inverses and correction provenance through real controls.
// Fault setup uses only the scratch library's normal service contracts.
import { execFileSync } from 'node:child_process'
import { join } from 'node:path'
import { close, launch, lib, resize, unthrottle, waitIndexed } from '../lib/app.mjs'
import { button, dismissNotices, selectAllResults, selectPhoto, undoNotice as notice, until } from '../lib/controls.mjs'
import { bindingID, mutationTrace } from '../lib/mutation-trace.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('undo-recovery')
const dataDir = join(s.run, 'data')
const photos = join(s.run, 'photos')
const PIN = '315926'
const rule = { kind: '', favourites: false, from: '', to: '', tag: 0, person: 0 }
let app
let trace
let phase = 'fixture'
let failure
const external = []
const consoleErrors = []
const refusedReads = []
const diagnostics = []
const reads = new Map([
  ...['Asset', 'AssetAlbums', 'Place'].map((name) => [bindingID('libraryservice', name), name]),
  [bindingID('searchservice', 'AssetTags'), 'AssetTags'],
  [bindingID('peopleservice', 'AssetFaces'), 'AssetFaces'],
  [bindingID('mediaservice', 'Inspect'), 'Inspect'],
])
function watch(page) {
  s.watch(page, { context: () => phase, expected: row => row.kind === 'response' && row.context === 'deleted album identity and failed-item recovery' && row.method === 'Inspect' && row.code === 'not_found' })
  page.on('pageerror', (error) => console.log(`PAGE ERROR ${error.stack ?? error}`))
  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push({ phase, text: message.text(), location: message.location(), stack: message.stackTrace() })
      console.log(`CONSOLE ERROR ${message.text()} ${JSON.stringify(message.location())}`)
    }
  })
  page.on('response', (response) => {
    if (response.status() < 400 || new URL(response.url()).pathname !== '/wails/runtime') return
    const request = JSON.parse(response.request().postData() ?? 'null')
    const name = reads.get(request?.args?.methodID)
    if (!name) return
    const current = { phase, read: name, id: request.args.args[0], status: response.status() }
    diagnostics.push(response.text().then(
      (error) => refusedReads.push({ ...current, error }),
      () => refusedReads.push(current),
    ))
  })
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname)) external.push(request.url())
  })
}
const root = (id) => `[data-notification="${id}"]`
async function caption(page, id, ...parts) {
  await until(() => page.$eval(root(id), (el, parts) => parts.every((part) => el.innerText.includes(part)), parts),
    `inverse ${id} did not report ${parts.join(', ')}`)
}
async function library(page) {
  await page.click('aside a[href="/"]')
  await page.waitForSelector('.scroller')
}
async function unblock(page, block) {
  await lib(page, 'UnlockBlocks', PIN)
  await lib(page, 'RemoveBlock', block.id)
  await lib(page, 'LockBlocks')
}
async function date(page, id, value) {
  await selectPhoto(page, id)
  await page.click('[data-edit-date]')
  await page.locator('#correct-date-input').fill(value)
  await button(page, 'Save date', '[role="dialog"]')
  await until(() => page.evaluate(() => !document.querySelector('[role="dialog"]')), 'date editor did not close')
  await trace.idle()
  return notice(page)
}
async function membershipBox(page, name, dialog = true) {
  const selector = `${dialog ? '[role="dialog"]' : '.album-option'} label::-p-text(${name})`
  await page.waitForSelector(selector, { visible: true })
  await page.click(selector)
}
try {
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', photos, '3'], { stdio: 'inherit' })
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await lib(app.page, 'AddSources', [photos])
  await waitIndexed(app.page, { atLeast: 3, timeout: 120_000, log: console.log })
  await lib(app.page, 'SetPaused', true)
  await close(app)
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  let page = app.page
  watch(page)
  await resize(page, 1440, 1000)
  await page.waitForSelector('.scroller .cell')
  const ids = (await lib(page, 'Selection', { filter: '' }, [], false)).ids
  s.must('three stable fixture targets are available', ids.length === 3, ids)
  const [first, second, third] = ids
  const albumA = await lib(page, 'CreateAlbum', 'Native album A', 'manual', rule, [])
  const albumB = await lib(page, 'CreateAlbum', 'Native album B', 'manual', rule, [])
  await lib(page, 'CreateBlockPIN', PIN)
  const original = await lib(page, 'Asset', first)
  const priorDate = Date.UTC(2001, 0, 1) / 1000
  await lib(page, 'SetTakenAt', [first], priorDate)
  await close(app)
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  page = app.page
  watch(page)
  await resize(page, 1440, 1000)
  await page.waitForSelector('.scroller .cell')
  trace = await mutationTrace(page)

  phase = 'selection album partial Undo'
  await selectAllResults(page)
  await page.waitForSelector('[data-command="library.albumSelection"]')
  await page.click('[data-command="library.albumSelection"]')
  await page.waitForSelector('[aria-label="Choose an album"]', { visible: true })
  await page.click('[aria-label="Choose an album"]')
  await page.waitForSelector('[role="option"]::-p-text(Native album A)')
  await page.click('[role="option"]::-p-text(Native album A)')
  await button(page, 'Add to album…', '[role="dialog"]')
  await until(async () => (await lib(page, 'AssetAlbums', second)).includes(albumA), 'selection membership did not persist')
  const selectionUndo = await notice(page)
  const blocked = await lib(page, 'SetBlock', 'asset', second, '', 'photos')
  trace.reset()
  trace.holdNext((row) => row.name === 'ApplyBatch' && row.action === 'removeAlbum' && row.album === albumA)
  await button(page, 'Undo', root(selectionUndo))
  await until(() => trace.held, 'selection inverse did not reach its write')
  await page.click('aside a[href="/things"]')
  await until(() => page.evaluate(() => location.pathname === '/things'), 'navigation did not complete while Undo was pending')
  await trace.release()
  await caption(page, selectionUndo, '2 restored', '1 failed')
  s.check('partial selection inverse reports both committed and refused targets after navigation',
    (await lib(page, 'Albums')).find((album) => album.id === albumA)?.count === 0 && (await lib(page, 'Counts')).photos === 2)
  await library(page)
  await unblock(page, blocked)
  await selectPhoto(page, third)
  const focus = await page.$eval('.scroller', (el) => el.getAttribute('aria-activedescendant'))
  s.check('the failed original membership remains after unblocking', (await lib(page, 'AssetAlbums', second)).includes(albumA))
  trace.reset()
  await button(page, 'Retry failed items', root(selectionUndo))
  await caption(page, selectionUndo, '3 restored', '0 failed')
  await trace.idle()
  const retried = trace.calls.filter((row) => row.write)
  s.check('selection retry targets only the original failed member and album', retried.length === 1 &&
    JSON.stringify(retried[0].ids) === JSON.stringify([second]) && retried[0].album === albumA, retried)
  s.check('selection retry keeps the new view focus',
    (await page.$eval('.scroller', (el) => el.getAttribute('aria-activedescendant'))) === focus)
  await dismissNotices(page)

  phase = 'multi-album picker partial Undo'
  await selectPhoto(page, first)
  await button(page, 'Add to album…', '.inspector-body')
  await membershipBox(page, 'Native album A')
  await membershipBox(page, 'Native album B')
  await button(page, 'Apply albums', '[role="dialog"]')
  const pickerUndo = await notice(page)
  await until(async () => (await lib(page, 'AssetAlbums', first)).length === 2, 'picker membership did not persist')
  trace.reset()
  trace.holdNext((row) => row.name === 'ApplyBatch' && row.action === 'removeAlbum' && row.album === albumB)
  await button(page, 'Undo', root(pickerUndo))
  await until(() => trace.held, 'the second picker inverse did not reach its write')
  const pickerBlock = await lib(page, 'SetBlock', 'asset', first, '', 'photos')
  await trace.release()
  await caption(page, pickerUndo, '1 restored', '1 failed')
  await button(page, 'View outcomes', root(pickerUndo))
  await page.waitForSelector('[role="dialog"] [data-undo-item]')
  s.check('picker outcomes retain both original album/asset identities', await page.$$eval('[data-undo-item]',
    (rows, albumA, albumB, asset) => rows.length === 2 && rows.some((row) => row.textContent.includes(`Asset ${asset} · album ${albumA}`)) &&
      rows.some((row) => row.textContent.includes(`Asset ${asset} · album ${albumB}`)), albumA, albumB, first))
  await page.screenshot({ path: join(s.out, 'partial-memberships.png') })
  await page.keyboard.press('Escape')
  await until(() => page.evaluate(() => !document.querySelector('[role="dialog"]')), 'picker outcomes did not close on Escape')
  await unblock(page, pickerBlock)
  const partial = await lib(page, 'AssetAlbums', first)
  s.check('picker inverse persists its earlier success and failed remainder', !partial.includes(albumA) && partial.includes(albumB), partial)
  trace.reset()
  await button(page, 'Retry failed items', root(pickerUndo))
  await caption(page, pickerUndo, '2 restored', '0 failed')
  await trace.idle()
  s.check('picker retry does not replay the completed membership', trace.calls.filter((row) => row.write).length === 1 &&
    trace.calls.some((row) => row.name === 'ApplyBatch' && row.album === albumB && row.ids[0] === first), trace.summary())
  await dismissNotices(page)

  phase = 'blocked album membership recovery'
  await selectPhoto(page, first)
  await membershipBox(page, 'Native album A', false)
  const hiddenAlbumUndo = await notice(page)
  await until(async () => (await lib(page, 'AssetAlbums', first)).includes(albumA), 'album membership did not persist')
  const albumBlock = await lib(page, 'SetBlock', 'album', albumA, '', 'label')
  await button(page, 'Undo', root(hiddenAlbumUndo))
  await caption(page, hiddenAlbumUndo, '0 restored', '1 failed')
  s.check('a hidden album refuses Undo while its photo remains visible',
    (await lib(page, 'Counts')).photos === 3 && !(await lib(page, 'Albums')).some((album) => album.id === albumA))
  await page.screenshot({ path: join(s.out, 'blocked-album-undo.png') })
  await unblock(page, albumBlock)
  s.check('refused Undo preserves the hidden album membership', (await lib(page, 'AssetAlbums', first)).includes(albumA))
  trace.reset()
  await button(page, 'Retry failed items', root(hiddenAlbumUndo))
  await caption(page, hiddenAlbumUndo, '1 restored', '0 failed')
  await trace.idle()
  s.check('unblocking retries only the original album membership',
    !(await lib(page, 'AssetAlbums', first)).includes(albumA) && trace.calls.filter((row) => row.write).length === 1 &&
    trace.calls.some((row) => row.name === 'ApplyBatch' && row.album === albumA && row.ids[0] === first), trace.summary())
  await dismissNotices(page)

  phase = 'date correction provenance and newer edits'
  const dateUndo = await date(page, first, '2002-01-01T00:00:00')
  await button(page, 'Undo', root(dateUndo))
  await caption(page, dateUndo, '1 restored', '0 failed')
  const restored = await lib(page, 'Asset', first)
  s.check('date Undo restores the user correction and retains original provenance', restored.takenAt === priorDate &&
    restored.dateFrom === 'user' && restored.originalDate?.takenAt === original.takenAt, restored)
  await dismissNotices(page)
  const olderDate = await date(page, first, '2002-01-01T00:00:00')
  await date(page, first, '2003-01-01T00:00:00')
  trace.reset()
  await button(page, 'Undo', root(olderDate))
  await caption(page, olderDate, 'newer edits kept: 1')
  const latestDate = Date.UTC(2003, 0, 1) / 1000
  s.check('a newer date edit supersedes the earlier inverse', (await lib(page, 'Asset', first)).takenAt === latestDate &&
    !trace.calls.some((row) => row.write), trace.summary())
  await dismissNotices(page)

  phase = 'self-covering place correction'
  const city = (await lib(page, 'FindCities', 'Karachi', 5))[0]
  s.must('the offline atlas supplies the place fixture', !!city, city)
  await lib(page, 'SetLocation', [third], city.lat, city.lon)
  const placeBlock = await lib(page, 'SetBlock', 'place', city.id, '', 'photos')
  await selectPhoto(page, first)
  await page.click('[data-edit-place]')
  await page.locator('input[aria-label="Latitude"]').fill(String(city.lat))
  await page.locator('input[aria-label="Longitude"]').fill(String(city.lon))
  await button(page, 'Save place', '[role="dialog"]')
  const placeUndo = await notice(page)
  await until(async () => (await lib(page, 'Counts')).photos === 1, 'place correction did not apply coverage')
  await until(() => page.evaluate((id) => !document.getElementById(`cell-${id}`), first), 'covered photo remained visible')
  await selectPhoto(page, second)
  trace.reset()
  await button(page, 'Undo', root(placeUndo))
  await caption(page, placeUndo, '1 restored', '0 failed')
  await trace.idle()
  const location = await lib(page, 'Asset', first)
  s.check('place Undo restores absent GPS even after the correction covers its own photo', location.lat === null &&
    location.lon === null && !location.originalLocation && (await lib(page, 'Counts')).photos === 2, location)
  s.check('self-covering place Undo uses the opaque original target', trace.calls.some((row) => row.name === 'RestoreLocation' &&
    row.ids[0] === first) && !trace.calls.some((row) => row.name === 'Asset' && row.ids[0] === first), trace.summary())
  await unblock(page, placeBlock)
  await dismissNotices(page)

  phase = 'deleted album identity and failed-item recovery'
  await selectPhoto(page, first)
  await trace.idle()
  trace.holdNext((row) => row.name === 'Inspect' && row.ids[0] === first)
  await membershipBox(page, 'Native album B', false)
  const obsolete = await notice(page)
  await until(() => trace.held, 'the current inspection did not reach its read')
  const identityBlock = await lib(page, 'SetBlock', 'asset', first, '', 'photos')
  await trace.release()
  await until(() => refusedReads.some((row) => row.phase === phase && row.read === 'Inspect' && row.id === first && row.status === 422),
    'the blocked inspection did not return its real visibility refusal')
  s.check('an in-flight inspection is refused when its original photo becomes blocked',
    refusedReads.some((row) => row.phase === phase && row.read === 'Inspect' && row.id === first && row.status === 422))
  await button(page, 'Undo', root(obsolete))
  await caption(page, obsolete, '1 failed')
  console.log('undo-recovery: delete the original album through its collection control')
  await page.click('aside a[href="/albums"]')
  await page.waitForSelector('button[aria-label="Actions for Native album B"]')
  await page.locator('button[aria-label="Actions for Native album B"]').click()
  await page.waitForSelector('[role="menuitem"]::-p-text(Delete “Native album B”)')
  await page.locator('[role="menuitem"]::-p-text(Delete “Native album B”)').click()
  await button(page, 'Delete album')
  await until(async () => !(await lib(page, 'Albums')).some((album) => album.id === albumB), 'album deletion did not persist')
  await unblock(page, identityBlock)
  await library(page)
  await selectPhoto(page, first)
  console.log('undo-recovery: open the replacement-album form')
  await button(page, 'New album for this item', '.inspector-body')
  await page.locator('input[aria-label="New album for this item"]').fill('Replacement album B')
  await page.keyboard.press('Enter')
  await until(async () => (await lib(page, 'Albums')).some((album) => album.name === 'Replacement album B'), 'replacement album was not created')
  const replacement = (await lib(page, 'Albums')).find((album) => album.name === 'Replacement album B')
  console.log('undo-recovery: verify replacement identity and retry the obsolete inverse')
  s.must('the replacement exercises SQLite album ID reuse', replacement.id === albumB, replacement)
  trace.reset()
  await button(page, 'Retry failed items', root(obsolete))
  await caption(page, obsolete, 'newer edits kept: 1', '0 failed')
  await trace.idle()
  s.check('a deleted album inverse never targets its replacement', (await lib(page, 'AssetAlbums', first)).includes(replacement.id) &&
    !trace.calls.some((row) => row.write), trace.summary())
  await dismissNotices(page)

  phase = 'restart persistence'
  await close(app)
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  const persisted = await lib(app.page, 'Asset', first)
  s.check('corrections and replacement membership persist across restart', persisted.takenAt === latestDate &&
    persisted.dateFrom === 'user' && persisted.originalDate?.takenAt === original.takenAt && persisted.lat === null &&
    persisted.lon === null && (await lib(app.page, 'AssetAlbums', first)).includes(replacement.id) &&
    (await lib(app.page, 'Counts')).photos === 3, persisted)
  s.check('no external requests', external.length === 0, external)
  s.check('bridge instrumentation completed without errors', trace.errors.length === 0, trace.errors)
  await s.settleErrors()
  s.check('no unexpected frontend errors', s.appErrors().length === 0, s.appErrors())
} catch (error) {
  failure = { phase, stack: error.stack ?? String(error) }
  console.log(error.stack ?? error)
  s.check(`Undo recovery completed (${phase})`, false, String(error))
  if (app) await app.page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  await trace?.release().catch(() => {})
  if (app) await close(app)
  await Promise.allSettled(diagnostics)
  s.finish({ failure, diagnostics: { consoleErrors, refusedReads } })
}
