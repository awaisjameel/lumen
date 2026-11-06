// Command admission and ownership through the production WebView, with only
// generated scratch files. A held real request proves pending and retry UI.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { close, go, launch, lib, resize, unthrottle, waitIndexed } from '../lib/app.mjs'
import { button, diagnostic, dismissNotices, until } from '../lib/controls.mjs'
import { bindingID, mutationTrace } from '../lib/mutation-trace.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('commands')
const dataDir = join(s.run, 'data'), photos = join(s.run, 'photos')
const errors = [], external = [], evidence = []
let app, page, trace, photo, album, failure
let phase = 'fixture'
function step(name) { phase = name; console.log(`commands: ${phase}`) }
function watch() {
  s.watch(page)
  page.on('pageerror', error => errors.push({ phase, error: String(error) }))
  page.on('request', request => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname)) external.push(url.href)
  })
}
async function start() {
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  page = app.page
  watch()
  await unthrottle(page)
  await resize(page, 1440, 1000)
}
async function capture(name) { await page.screenshot({ path: join(s.out, `${name}.png`) }) }
async function path(expected) { await until(() => page.evaluate(expected => location.pathname === expected, expected), `missing path ${expected}`) }
async function palette(query, choose = true) {
  await page.keyboard.press('Control+k')
  const input = await page.waitForSelector('[role="dialog"][data-state="open"] input')
  await input.asLocator().fill(query)
  await page.waitForFunction((query, choose) => {
    const normal = value => value.toLowerCase().replace(/[^\p{L}\p{N}]/gu, '')
    return [...document.querySelectorAll('[role="option"]')].some(row => normal(row.textContent).includes(normal(query))) ||
      (!choose && document.querySelector('.palette .empty'))
  }, {}, query, choose)
  if (choose) {
    await page.locator('[role="option"]').click()
    await until(() => page.$$eval('[role="dialog"][data-state="open"]', rows => rows.length === 0), 'palette did not close')
  }
}
async function settings() {
  await go(page, '/settings?section=accessibility', 0)
  await page.waitForSelector('[data-shortcut="viewer.rotate"]')
}
async function record(id, keys) {
  await page.locator(`[data-shortcut="${id}"] [data-shortcut-change]`).click()
  await page.keyboard.press(keys)
}
async function viewer() {
  await go(page, '/', 0)
  await page.waitForSelector(`#cell-${photo.id}`, { visible: true })
  await page.locator(`#cell-${photo.id}`).click({ count: 2 })
  await page.waitForSelector('.viewer [data-command="viewer.rotate"]')
  await until(() => page.$eval('.viewer [data-command="viewer.rotate"]', node => !node.disabled), 'viewer did not load')
  await page.locator('.viewer').click({ offset: { x: 20, y: 100 } })
}
async function closeViewer() {
  await page.keyboard.press('Escape')
  await page.waitForSelector('.viewer', { hidden: true })
}
const prefs = () => lib(page, 'Preferences')
const rotation = async () => (await lib(page, 'Asset', photo.id)).adjustment?.rotation ?? 0
const hash = file => createHash('sha256').update(readFileSync(file)).digest('hex')

try {
  mkdirSync(photos, { recursive: true })
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', photos, '3'], { stdio: 'inherit' })
  await start()
  await lib(page, 'AddSources', [photos])
  await waitIndexed(page, { ai: true, atLeast: 3, timeout: 120_000, log: console.log })
  await lib(page, 'SetPaused', true)
  const buckets = await lib(page, 'Timeline', { filter: '' })
  photo = (await lib(page, 'Days', buckets[0].day, buckets[0].day, { filter: '' }))[0].items[0]
  const original = (await lib(page, 'Asset', photo.id)).paths[0]
  const originalHash = hash(original)
  album = await lib(page, 'CreateAlbum', 'Command before', 'manual', { kind: '', favourites: false, from: '', to: '', tag: 0, person: 0 }, [photo.id])
  await lib(page, 'SetPreferences', { ...await prefs(), shortcuts: { 'legacy.action': 'Ctrl+L', 'go.people': 'Ctrl+G' } })
  await page.waitForNetworkIdle()
  await page.reload()
  await page.waitForSelector('aside.sidebar')

  step('palette, focus and imported overrides')
  await palette('Go to People')
  await path('/people')
  await go(page, '/', 0)
  await page.waitForSelector('.scroller')
  await palette('Search your photos')
  await until(() => page.evaluate(() => document.activeElement?.matches('input[type="search"]')), 'search command did not receive focus after palette dismissal')
  s.check('palette dismisses before the search command takes focus', true)
  await page.keyboard.press('Escape')
  await page.locator('main').click({ offset: { x: 20, y: 20 } })
  await page.keyboard.press('Control+g')
  await path('/people')
  s.check('an imported known command outside the shortcut editor remains active', true)
  await capture('palette-focus')

  step('shortcut recording, conflicts and help')
  await settings()
  await record('viewer.rotate', 'Control+r')
  await until(async () => (await prefs()).shortcuts?.['viewer.rotate'] === 'Ctrl+R', 'shortcut did not persist')
  await record('viewer.flip', 'Control+r')
  await page.waitForSelector('[data-shortcut="viewer.flip"] [role="alert"]')
  s.check('conflicting reassignment is refused without losing the first binding', (await prefs()).shortcuts?.['viewer.flip'] === undefined)
  s.check('fixed Escape offers no reassignment control', !(await page.$('[data-shortcut="viewer.close"] [data-shortcut-change]')))
  await page.locator('main').click({ offset: { x: 20, y: 20 } })
  await page.keyboard.press('?')
  await page.waitForSelector('[role="dialog"][data-state="open"]')
  s.check('help derives the active reassignment', await page.$$eval('[role="dialog"]', rows => rows.some(row => /Rotate/.test(row.textContent) && /Ctrl/.test(row.textContent))))
  await page.keyboard.press('Escape')
  await capture('shortcut-editor')

  step('pending admission and independent commands')
  await viewer()
  await page.keyboard.press('Control+k')
  s.check('the viewer retains the keyboard instead of opening the page palette', !(await page.$('[role="dialog"][data-state="open"] input')))
  const zoom = () => page.$eval('[data-zoom]', node => Number.parseFloat(node.textContent))
  const fit = await zoom()
  await page.keyboard.down('=')
  await page.keyboard.down('=')
  await page.keyboard.up('=')
  await until(async () => await zoom() > fit * 1.4, 'held repeatable zoom did not run twice')
  s.check('held keys keep explicitly repeatable zoom responsive', true)
  trace = await mutationTrace(page, undefined, new Map(), new Map([[bindingID('libraryservice', 'SetAdjustment'), 'SetAdjustment']]))
  const before = await rotation()
  trace.reset()
  trace.holdNext(row => row.name === 'SetAdjustment')
  await page.keyboard.press('Control+r')
  await until(() => trace.held, 'the real adjustment request was not held')
  await until(() => page.$$eval('.viewer [data-command="viewer.rotate"]', nodes => nodes.length > 0 && nodes.every(node => node.disabled)), 'pending controls were not disabled')
  await page.keyboard.press('Control+r')
  await page.keyboard.press('Control+r')
  await page.keyboard.press('i')
  await page.waitForSelector('.viewer .inspector', { visible: true })
  s.check('pending work is admitted once while an independent viewer command still runs', trace.calls.filter(row => row.name === 'SetAdjustment').length === 1)
  await capture('pending-controls')
  await trace.release()
  await until(async () => await rotation() === (before + 1) % 4, 'rotation did not complete')
  await until(() => page.$eval('.viewer [data-command="viewer.rotate"]', node => !node.disabled), 'completion did not restore the control')
  s.check('the completed action restores admission', true)

  step('real transport failure and retry')
  await dismissNotices(page)
  trace.reset()
  trace.holdNext(row => row.name === 'SetAdjustment')
  await page.locator('.viewer [data-command="viewer.rotate"]').click()
  await until(() => trace.held, 'the retry fixture request was not held')
  await trace.abortHeld()
  await until(() => page.$$eval('[data-notification][data-state="open"]', rows => rows.length === 1), 'the refused action did not report one failure')
  await until(() => page.$eval('.viewer [data-command="viewer.rotate"]', node => !node.disabled), 'failure did not restore the button')
  await capture('failure-retry')
  await page.locator('.viewer [data-command="viewer.rotate"]').click()
  await until(async () => await rotation() === (before + 2) % 4, 'retry did not complete')
  s.check('one failed request reports once and the same button succeeds on retry', trace.calls.filter(row => row.name === 'SetAdjustment').length === 2)
  await trace.idle()
  evidence.push({ phase, writes: trace.calls.filter(row => row.name === 'SetAdjustment').length, instrumentationErrors: trace.errors })
  await closeViewer()
  s.check('closing the viewer returns focus to its grid', await page.evaluate(() => document.activeElement?.closest('.scroller') !== null))

  step('contextual item menu and current palette identity')
  await go(page, '/albums', 0)
  await page.locator('button[aria-label="Actions for Command before"]').click()
  await page.locator('[role="menuitem"]::-p-text(Edit)').click()
  const name = await page.waitForSelector('#album-editor input')
  await until(() => page.evaluate(() => document.activeElement?.closest('#album-editor') !== null), 'the item editor did not receive focus')
  await name.asLocator().fill('Command after')
  await button(page, 'Save changes')
  await until(async () => (await lib(page, 'Album', album)).name === 'Command after', 'item edit did not commit')
  await palette('Edit Command after', false)
  s.check('the refreshed item command has the current context label', await page.$$eval('[role="option"]', rows => rows.some(row => row.textContent.includes('Command after')) && rows.every(row => !row.textContent.includes('Command before'))))
  await page.keyboard.press('Escape')
  await go(page, '/', 0)
  await page.waitForSelector('.scroller')
  await palette('Command after', false)
  s.check('leaving the collection retires its item commands', (await page.$$('[role="option"]')).length === 0)
  await page.keyboard.press('Escape')

  step('restart and reset compatibility')
  await page.waitForNetworkIdle()
  await close(app)
  app = undefined
  await start()
  await page.waitForSelector('aside.sidebar')
  const saved = await prefs()
  s.check('restart preserves known and unknown imported overrides', saved.shortcuts?.['viewer.rotate'] === 'Ctrl+R' && saved.shortcuts?.['go.people'] === 'Ctrl+G' && saved.shortcuts?.['legacy.action'] === 'Ctrl+L')
  await viewer()
  const prior = await rotation()
  await page.keyboard.press('Control+r')
  await until(async () => await rotation() === (prior + 1) % 4, 'the stored shortcut did not execute after restart')
  s.check('the reassigned shortcut executes after restart', true)
  await closeViewer()
  await settings()
  await page.locator('[data-shortcut-reset-all]').click()
  await until(async () => Object.keys((await prefs()).shortcuts ?? {}).length === 0, 'Reset all retained stored overrides')
  s.check('Reset all explicitly clears known and legacy overrides', true)
  s.check('commands preserve the original file bytes', hash(original) === originalHash)
  await capture('reset-all')
} catch (error) {
  failure = String(error)
  s.check(`commands acceptance: ${phase}`, false, failure)
  if (page) {
    evidence.push({ phase, state: await diagnostic(page.evaluate(() => ({ path: location.pathname, text: document.body.innerText.slice(-1200) }))) })
    await page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
  }
} finally {
  if (trace?.held) await trace.release().catch(() => {})
  if (app) await close(app)
}
s.check('no uncaught page errors escape the command owners', errors.length === 0, errors)
s.check('no unexpected console errors escape the feature owners', s.appErrors().length === 0, s.appErrors())
s.check('no request leaves the PC', external.length === 0, external)
s.finish({ failure, evidence, pageErrors: errors, external })
