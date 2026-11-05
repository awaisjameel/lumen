// Local editor admission and guarded writes under real privacy invalidation.
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { close, launch, lib, resize, search, unthrottle, waitIndexed } from '../lib/app.mjs'
import { button, diagnostic, dismissNotices, option, until } from '../lib/controls.mjs'
import { copyLibrary } from '../lib/fixtures.mjs'
import { bindingID, mutationTrace } from '../lib/mutation-trace.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('album-draft')
const seed = join(s.run, 'seed')
const photos = join(s.run, 'photos')
const rule = { kind: '', favourites: false, from: '', to: '', tag: 0, person: 0 }
const reads = new Map([[bindingID('libraryservice', 'Albums'), 'Albums']])
const writes = new Map(['SaveAlbum', 'CreateAlbum'].map(name => [bindingID('libraryservice', name), name]))
const pageErrors = []
const diagnostics = []
const consoleReads = []
const external = []
const observations = []
const histories = []
let app
let trace
let dataDir = seed
let readFault = false
let saveFault = false
let phase = 'seed'
let action = 'seed'
let failure
let a
let b
let tag
let initialName = 'Native draft A'
const otherName = 'Native draft B'
const input = '#album-editor input[aria-label="Album name"]'

function step(value) {
  action = value
  console.log(`album-draft: ${phase}: ${value}`)
}
function fault(value) {
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'album_draft.py'), value,
    join(dataDir, 'library.db')], { stdio: 'inherit' })
  if (value.endsWith('read')) readFault = value === 'refuse-read'
  else saveFault = value === 'refuse-save'
}
function watch(page) {
  s.watch(page)
  page.on('pageerror', error => pageErrors.push({ phase, error: String(error) }))
  page.on('console', message => {
    if (message.type() !== 'error') return
    const current = { phase, fault: readFault }
    consoleReads.push(Promise.all(message.args().map(value => value.evaluate(value =>
      value && typeof value.message === 'string' ? value.message : String(value)).catch(() => message.text()))).then(parts =>
      diagnostics.push({ ...current, error: parts.join(' ') || message.text() })))
  })
  page.on('request', request => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname)) external.push(url.href)
  })
}
async function start(label, baseline) {
  dataDir = copyLibrary(seed, join(s.run, label))
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await app.page.waitForSelector('.scroller .cell', { visible: true })
  trace = await mutationTrace(app.page, baseline, reads, writes)
  await app.page.reload()
  await app.page.waitForSelector('.scroller .cell', { visible: true })
  await app.page.bringToFront()
  await resize(app.page, 1440, 1000)
  await app.page.locator('aside a[href="/albums"]').click()
  await app.page.waitForSelector('[data-action="new-album"]', { visible: true })
}
function resetTrace() {
  histories.push({ phase, calls: trace.calls })
  trace.reset()
}
async function stop() {
  if (trace) histories.push({ phase, calls: trace.calls })
  if (readFault) fault('restore-read')
  if (saveFault) fault('restore-save')
  if (trace?.held) await trace.release()
  if (app) await close(app)
  app = undefined
}
async function menu(name, text, root = '') {
  step(`open ${text}`)
  await app.page.locator(`${root} button[aria-label^="Actions for"][aria-label*="${name}"]`).click()
  await app.page.locator(`[role="menuitem"]::-p-text(${text})`).click()
}
async function edit(name = initialName) {
  await menu(name, `Edit “${name}”`)
  await app.page.waitForSelector(input, { visible: true })
}
async function nameValue() { return app.page.$eval(input, field => field.value) }
async function fill(value) {
  step(`fill ${value}`)
  await app.page.locator(input).fill(value)
}
async function saved(id = a) { return (await lib(app.page, 'Albums')).find(album => album.id === id) }
async function blockOther() {
  await dismissNotices(app.page)
  await menu(otherName, `Block “${otherName}”?`)
  await app.page.locator('label::-p-text(Hide the name only)').click()
  await app.page.locator('[data-block="save"]').click()
  await app.page.waitForSelector('[data-notification="block.undo"]', { visible: true })
}
async function undoHeld() {
  await trace.idle()
  resetTrace()
  trace.holdNext(row => row.name === 'Albums')
  step('Undo the unrelated block while the edited draft remains open')
  await button(app.page, 'Undo', '[data-notification="block.undo"]')
  await until(() => trace.held, 'the privacy invalidation did not reach Albums')
}
async function saveDraft() {
  step('save the current draft')
  await app.page.locator('button[form="album-editor"]').click()
}
async function closed() { await app.page.waitForSelector('#album-editor', { hidden: true }) }

try {
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', photos, '2'], { stdio: 'inherit' })
  app = await launch({ exe: s.exe, dataDir: seed, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await lib(app.page, 'AddSources', [photos])
  await waitIndexed(app.page, { ai: true, atLeast: 2, timeout: 120_000, log: console.log })
  await lib(app.page, 'SetPaused', true)
  const ids = (await lib(app.page, 'Selection', { filter: '' }, [], false)).ids
  s.must('two generated originals are indexed', ids.length === 2, ids)
  await search(app.page, 'TagAssets', ids, 'Native draft reference')
  tag = (await search(app.page, 'Things')).find(thing => thing.name === 'Native draft reference').id
  b = await lib(app.page, 'CreateAlbum', otherName, 'manual', rule, ids)
  // Delete the highest ID later, so SQLite's normal ID reuse is exercised.
  a = await lib(app.page, 'CreateAlbum', initialName, 'smart', { ...rule, tag }, [])
  await lib(app.page, 'CreateBlockPIN', '612435')
  await stop()

  if (process.env.LUMEN_BASELINE_FRONTEND) {
    phase = 'built parent reproduction'
    // Select expectations from the parent's source contract before observing
    // outcomes, so later children also verify the inherited repaired parent.
    const admitted = readFileSync(join(process.env.LUMEN_BASELINE_FRONTEND, '..', 'src', 'albums', 'AlbumsView.vue'), 'utf8').includes('data-draft-state')
    await start('baseline', process.env.LUMEN_BASELINE_FRONTEND)
    await blockOther()
    await edit()
    await fill('Parent local draft')
    await undoHeld()
    await trace.release()
    await trace.idle()
    await app.page.waitForSelector(input, { visible: true })
    const actual = await nameValue()
    const label = await app.page.$eval('button[form="album-editor"]', node => node.textContent.trim())
    s.check(admitted ? 'built parent preserves its admitted edit contract' : 'built parent reproduces loss of the valid draft and switches into creation',
      admitted ? actual === 'Parent local draft' && label === 'Save changes' : actual === '' && label === 'Create album', { admitted, actual, label })
    observations.push({ phase, admitted, actual, label })
    await app.page.screenshot({ path: join(s.out, 'parent-after-unrelated-undo.png') })
    s.check('parent instrumentation has no errors', trace.errors.length === 0, trace.errors)
    await stop()
  }

  phase = 'unrelated delayed privacy Undo'
  await start('candidate')
  await blockOther()
  await edit()
  await fill('Retained native draft')
  await app.page.$eval(input, field => { field.setSelectionRange(3, 7); field.dispatchEvent(new Event('select', { bubbles: true })) })
  await undoHeld()
  s.check('pending privacy reads remove the private form and preview', !(await app.page.$('#album-editor')) && !(await app.page.$('[data-album-preview]')))
  await trace.release()
  await trace.idle()
  await app.page.waitForSelector(input, { visible: true })
  s.check('a visible album resumes its latest local fields and edit mode', await nameValue() === 'Retained native draft' &&
    await app.page.$eval('button[form="album-editor"]', node => node.textContent.includes('Save changes')))
  // A still-focused notification owns focus. If it has disappeared, the
  // editor restores its last field and caret instead of leaving focus lost.
  const focus = await app.page.evaluate(() => {
    const field = document.querySelector('#album-editor input[aria-label="Album name"]')
    return { field: document.activeElement === field, start: field.selectionStart, end: field.selectionEnd,
      notification: !!document.activeElement?.closest('[data-notification]') }
  })
  s.check('readmission respects the current focus owner or restores the draft caret',
    focus.notification || (focus.field && focus.start === 3 && focus.end === 7), focus)
  await saveDraft()
  await closed()
  s.check('the retained draft saves the original ID without creating an album', (await saved()).name === 'Retained native draft' &&
    trace.calls.filter(row => row.name === 'SaveAlbum').length === 1 && !trace.calls.some(row => row.name === 'CreateAlbum'))
  initialName = 'Retained native draft'

  phase = 'failed privacy read and read-only retry'
  await blockOther()
  await edit()
  await fill('Draft after failed read')
  await undoHeld()
  fault('refuse-read')
  await trace.release()
  await app.page.waitForSelector('[data-action="retry-draft"]', { visible: true })
  s.check('a refused read keeps the private draft out of the DOM and disables saving',
    !(await app.page.$('#album-editor')) && await app.page.$eval('button[form="album-editor"]', node => node.disabled))
  await app.page.screenshot({ path: join(s.out, 'paused-draft-after-read-failure.png') })
  await trace.idle()
  s.check('the real album read fails in the scratch database', trace.calls.some(row => row.name === 'Albums' && row.status >= 400))
  fault('restore-read')
  resetTrace()
  await app.page.locator('[data-action="retry-draft"]').click()
  await app.page.waitForSelector(input, { visible: true })
  await trace.idle()
  s.check('retry restores the same local draft without writing', await nameValue() === 'Draft after failed read' &&
    !trace.calls.some(row => row.write))
  await app.page.keyboard.press('Escape')
  await closed()
  s.check('Escape cancels without writing and returns focus to an available trigger', !trace.calls.some(row => row.write) &&
    await app.page.evaluate(() => document.activeElement?.getAttribute('data-action') === 'new-album'))

  phase = 'failed save and retry'
  await edit()
  await fill('Retried native save')
  fault('refuse-save')
  resetTrace()
  await saveDraft()
  await until(() => app.page.$eval('#album-editor', node => node.textContent.includes('album draft write refused')), 'the save failure was not explained')
  s.check('a refused write keeps the current local draft and intended ID', await nameValue() === 'Retried native save' && (await saved()).name === initialName)
  fault('restore-save')
  await saveDraft()
  await closed()
  s.check('retry saves exactly that draft to the original entity', (await saved()).name === 'Retried native save' &&
    trace.calls.filter(row => row.name === 'SaveAlbum').length === 2 &&
    trace.calls.filter(row => row.name === 'SaveAlbum').every(row => row.parameters[0].id === a) && !trace.calls.some(row => row.name === 'CreateAlbum'))
  initialName = 'Retried native save'

  phase = 'save-window conflict'
  await edit()
  await fill('Stale local draft')
  trace.holdNext(row => row.name === 'SaveAlbum')
  await saveDraft()
  await until(() => trace.held, 'the editor did not reach its guarded save')
  const before = await saved()
  await lib(app.page, 'SaveAlbum', { id: a, version: before.version, name: 'Concurrent saved name', rule: null })
  await trace.release()
  await app.page.waitForSelector('[data-action="reload-album"]', { visible: true })
  s.check('a save-window conflict preserves local input and cannot overwrite the newer definition', await nameValue() === 'Stale local draft' &&
    (await saved()).name === 'Concurrent saved name')
  await app.page.locator('[data-action="reload-album"]').click()
  await app.page.locator('[data-confirm="ok"]').click()
  await until(() => nameValue().then(value => value === 'Concurrent saved name'), 'explicit reload did not load the saved definition')
  s.check('explicit confirmed reload adopts the current saved version', await nameValue() === 'Concurrent saved name')
  await app.page.locator('[data-action="cancel-album"]').click()
  await closed()
  initialName = 'Concurrent saved name'

  phase = 'hidden selected reference'
  await app.page.locator('aside a[href="/things"]').click()
  await menu('Native draft reference', 'Block “Native draft reference”?')
  await app.page.locator('label::-p-text(Hide the name only)').click()
  await app.page.locator('[data-block="save"]').click()
  await app.page.waitForSelector('[data-notification="block.undo"]', { visible: true })
  await app.page.locator('aside a[href="/albums"]').click()
  await edit()
  s.check('a hidden selected Thing loses its name and requires explicit replacement',
    !(await app.page.$eval('#album-editor', node => node.textContent.includes('Native draft reference'))) &&
    await app.page.$eval('#album-editor', node => node.textContent.includes('Unavailable — choose another option')) &&
    await app.page.$eval('button[form="album-editor"]', node => node.disabled))
  await app.page.locator('#album-editor button[aria-label="Thing"]').click()
  await option(app.page, 'Any Thing')
  await saveDraft()
  await closed()
  s.check('an explicit rule replacement saves without the hidden identity', (await saved()).rule.tag === 0)
  await button(app.page, 'Undo', '[data-notification="block.undo"]')
  await trace.idle()

  phase = 'navigation during save'
  await edit()
  await fill('Saved after navigation')
  trace.holdNext(row => row.name === 'SaveAlbum')
  await saveDraft()
  await until(() => trace.held, 'the navigation save was not held')
  await app.page.locator('aside a[href="/things"]').click()
  await app.page.waitForSelector('[data-action="new-album"]', { hidden: true })
  await trace.release()
  await trace.idle()
  s.check('a completed abandoned save cannot reopen or navigate its old editor',
    (await saved()).name === 'Saved after navigation' && await app.page.evaluate(() => location.pathname === '/things') && !(await app.page.$('#album-editor')))
  initialName = 'Saved after navigation'

  phase = 'deleted target and reused ID'
  await app.page.locator('aside a[href="/albums"]').click()
  await edit()
  await fill('Must never reach replacement')
  trace.holdNext(row => row.name === 'SaveAlbum')
  await saveDraft()
  await until(() => trace.held, 'the deleted target save was not held')
  const old = await saved()
  await lib(app.page, 'DeleteAlbum', a)
  const replacement = await lib(app.page, 'CreateAlbum', initialName, 'smart', old.rule, [])
  s.must('the scratch replacement reuses the numeric ID', replacement === a, { replacement, a })
  await trace.release()
  await closed()
  await trace.idle()
  const current = await saved()
  s.check('a stale editor cannot rename the replacement entity or switch into creation', current.name === initialName &&
    current.version.identity !== old.version.identity && !(await app.page.$('#album-editor')))
  await app.page.screenshot({ path: join(s.out, 'replacement-entity-after-rejected-save.png') })

  phase = 'explicit target privacy'
  await edit()
  await menu(initialName, `Block “${initialName}”?`, '[role="dialog"]')
  await app.page.locator('label::-p-text(Hide the name only)').click()
  await app.page.locator('[data-block="save"]').click()
  await closed()
  s.check('explicit target blocking closes and clears the protected draft', !(await app.page.$('#album-editor')) && !(await saved()))
  await button(app.page, 'Undo', '[data-notification="block.undo"]')
  await until(async () => !!(await saved()), 'the target did not return after block Undo')
  await stop()
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await app.page.waitForSelector('.scroller .cell', { visible: true })
  trace = await mutationTrace(app.page, undefined, reads, writes)
  await app.page.locator('aside a[href="/albums"]').click()
  await app.page.waitForSelector('[data-action="new-album"]', { visible: true })
  s.check('restart retains only committed definitions and no abandoned draft', (await saved()).name === initialName && !(await app.page.$('#album-editor')))
  s.check('candidate instrumentation has no errors', trace.errors.length === 0, trace.errors)
  s.check('no external requests', external.length === 0, external)
  await Promise.all(consoleReads)
  const unexpected = diagnostics.filter(row => !/custom\.js|Failed to load resource/.test(row.error) &&
    !(row.phase === 'failed privacy read and read-only retry' && /^no such column: identity$/.test(row.error)))
  s.check('only the deliberately refused album read is logged, with no uncaught errors', pageErrors.length === 0 && unexpected.length === 0, { pageErrors, unexpected })
} catch (error) {
  failure = { phase, action, stack: error.stack ?? String(error) }
  s.check(`album draft completed (${phase})`, false, String(error))
  if (app) {
    await diagnostic(app.page.screenshot({ path: join(s.out, 'failure.png') }))
    failure.dom = await diagnostic(app.page.evaluate(() => ({ url: location.href, body: document.body.innerText.slice(0, 16000) })))
  }
} finally {
  await stop().catch(error => { failure ??= { phase, error: String(error) } })
  await Promise.all(consoleReads)
  s.finish({ failure, observations, diagnostics, pageErrors, histories })
}
