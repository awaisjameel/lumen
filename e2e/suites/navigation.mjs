// Real production destinations, browser input boundaries and navigation owners.
import { execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { close, launch, lib, people, resize, search, unthrottle, waitIndexed } from '../lib/app.mjs'
import { button, diagnostic, until } from '../lib/controls.mjs'
import { bindingID, mutationTrace } from '../lib/mutation-trace.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('navigation')
const dataDir = join(s.run, 'data')
const home = join(s.run, 'profile')
const photos = join(home, 'Pictures')
const PIN = '736425'
const rule = { kind: '', favourites: false, from: '', to: '', tag: 0, person: 0 }
const errors = []
const diagnostics = []
const consoleReads = []
const external = []
const visited = new Set()
let app
let trace
let phase = 'onboarding'
let failure
let origin
let person
let tag
let place
let event
let album
let smart
let first

function step(value) { console.log(`navigation: ${phase}: ${value}`) }
function watch(page) {
  s.watch(page)
  page.on('pageerror', error => errors.push({ phase, error: String(error) }))
  page.on('console', message => {
    if (message.type() !== 'error') return
    const current = phase
    consoleReads.push(Promise.all(message.args().map(value => value.evaluate(value =>
      value && typeof value.message === 'string' ? value.message : String(value)).catch(() => message.text()))).then(parts =>
      diagnostics.push({ phase: current, error: parts.join(' ') || message.text() })))
  })
  page.on('request', request => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname)) external.push(url.href)
  })
}
async function start() {
  // Only this process sees the synthetic profile. SuggestedFolders therefore
  // offers our generated Pictures directory, with no native dialog stubbing.
  app = await launch({ exe: s.exe, dataDir, port: s.port, env: { USERPROFILE: home } })
  watch(app.page)
  await unthrottle(app.page)
  await app.page.bringToFront()
  await resize(app.page, 1440, 1000)
  origin = new URL(app.page.url()).origin
}
async function stop() { if (app) await close(app); app = undefined }
async function path(expected) {
  await until(() => app.page.evaluate(expected => location.pathname + location.search === expected, expected), `destination did not become ${expected}`)
}
async function incoming(url, ready) {
  step(`incoming ${url}`)
  const response = await app.page.goto(origin + url)
  s.must(`the embedded server serves ${url}`, response?.status() === 200, response?.status())
  await path(ready ?? url)
}
async function sidebar(url) {
  step(`sidebar ${url}`)
  await app.page.locator(`aside a[href="${url}"]`).click()
  await path(url)
}
async function header(name) {
  await until(() => app.page.$$eval('h1', (headings, name) => headings.some(h => h.textContent.trim() === name), name), `missing destination heading ${name}`)
}
async function settings(section) {
  await incoming(`/settings?section=${section}`)
  await app.page.waitForSelector(`[data-category="${section}"]`, { visible: true })
}

async function unlock() {
  await app.page.locator('input[type="password"]').fill(PIN)
  await button(app.page, 'Unlock')
  await app.page.waitForSelector('[data-block-scroller]', { visible: true })
}

try {
  mkdirSync(photos, { recursive: true })
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', photos, '3'], { stdio: 'inherit' })
  await start()
  await app.page.waitForSelector('[data-welcome]', { visible: true })
  const suggested = await lib(app.page, 'SuggestedFolders')
  s.must('first run recommends only the generated scratch profile', suggested.length === 1 && suggested[0].replaceAll('\\', '/').toLowerCase() === photos.replaceAll('\\', '/').toLowerCase())
  s.check('onboarding has no navigation shell and adds no source implicitly', !(await app.page.$('aside')) && (await lib(app.page, 'Sources') ?? []).length === 0)
  await app.page.locator('[data-continue]').click()
  await app.page.locator('[data-continue]').click()
  await app.page.locator('[data-start]').click()
  await app.page.waitForSelector('[data-first-scan]', { visible: true })
  await waitIndexed(app.page, { ai: true, atLeast: 3, timeout: 120_000, log: console.log })
  await app.page.locator('[data-browse]').click()
  await app.page.waitForSelector('.scroller .cell', { visible: true })
  await path('/')
  s.check('the real onboarding handoff replaces the page with Library', !(await app.page.$('[data-welcome]')) && !!(await app.page.$('aside')))
  visited.add('library')
  await lib(app.page, 'SetPaused', true)
  await lib(app.page, 'SetVideos', true)
  const ids = (await lib(app.page, 'Selection', { filter: '' }, [], false)).ids
  first = ids[0]
  await lib(app.page, 'SetFavourite', first, true)
  await search(app.page, 'TagByName', ids, 'Navigation Thing')
  tag = (await search(app.page, 'Things')).find(t => t.name === 'Navigation Thing').id
  const faces = JSON.parse(execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'album_preview.py'), 'faces', join(dataDir, 'library.db'), JSON.stringify(ids)], { encoding: 'utf8' }))
  person = await people(app.page, 'NameFaces', faces, 'Navigation Person')
  place = (await lib(app.page, 'FindCities', 'Karachi', 5))[0]
  await lib(app.page, 'SetLocation', ids, place.lat, place.lon)
  event = (await lib(app.page, 'Events'))[0]
  album = await lib(app.page, 'CreateAlbum', 'Navigation Album', 'manual', rule, ids)
  smart = await lib(app.page, 'CreateAlbum', 'Navigation Smart', 'smart', rule, [])
  await lib(app.page, 'CreateBlockPIN', PIN)
  await app.page.reload()
  await app.page.waitForSelector('.scroller .cell', { visible: true })

  phase = 'production destinations and controls'
  for (const [name, url, title] of [
    ['favourites', '/favourites', 'Favourites'], ['people', '/people', 'People'], ['things', '/things', 'Things'],
    ['places', '/places', 'Places'], ['events', '/events', 'Events'], ['albums', '/albums', 'Albums'],
    ['duplicates', '/duplicates', 'Duplicates'], ['compression', '/compression', 'Compression'], ['trash', '/trash', 'Trash'],
    ['settings', '/settings', 'Settings'],
  ]) {
    await sidebar(url)
    await header(title)
    s.check(`${name}: a real shell link opens and highlights its own destination`, await app.page.$eval(`aside a[href="${url}"]`, a => a.getAttribute('aria-current') === 'page'))
    visited.add(name)
  }
  await sidebar('/people')
  await app.page.locator(`a[href="/people/${person}"]`).click()
  await path(`/people/${person}`)
  await header('Navigation Person')
  visited.add('person')
  await app.page.locator(`a[href="/people/${person}/faces"]`).click()
  await path(`/people/${person}/faces`)
  await app.page.waitForSelector('[data-face]', { visible: true })
  visited.add('personFaces')
  s.check('person face review keeps its People parent highlighted', await app.page.$eval('aside a[href="/people"]', a => a.getAttribute('aria-current') === 'page'))
  for (const [name, list, url, title] of [
    ['thing', '/things', `/things/${tag}`, 'Navigation Thing'],
    ['album', '/albums', `/albums/${album}`, 'Navigation Album'],
    ['place', '/places', `/places/${place.id}`, place.name],
    ['event', '/events', `/events/${event.id}`, event.name],
  ]) {
    await sidebar(list)
    if (name === 'place') {
      await app.page.locator(`[data-collection="${place.id}"] button[aria-label="Preview ${place.name}"]`).click()
      await app.page.waitForSelector('#place-preview', { visible: true })
      await app.page.locator(`#place-preview a[href="${url}"]`).click()
    } else await app.page.locator(`a[href="${url}"]`).click()
    await path(url)
    await header(title)
    s.check(`${name}: the collection control reaches the exact entity`, await app.page.$$eval('.scroller .cell', rows => rows.length > 0))
    visited.add(name)
  }
  await sidebar('/albums')
  await app.page.locator(`a[href="/albums/${smart}"]`).click()
  await button(app.page, 'Edit rule…', '.collection-hero')
  await app.page.waitForSelector('#album-editor', { visible: true })
  s.check('collection edit handoff consumes the canonical album query', await app.page.evaluate(id => location.pathname === '/albums' && new URLSearchParams(location.search).get('edit') === String(id), smart))
  await app.page.locator('[data-action="cancel-album"]').click()
  await path('/albums')

  phase = 'search, commands, media and history'
  await sidebar('/')
  await app.page.locator('input[role="combobox"]').fill('海辺 کی شام')
  await app.page.keyboard.press('Enter')
  await until(() => app.page.evaluate(() => location.pathname === '/search' && new URLSearchParams(location.search).get('q') === '海辺 کی شام'), 'Unicode search did not reach Search')
  visited.add('search')
  await app.page.keyboard.down('Control'); await app.page.keyboard.press('K'); await app.page.keyboard.up('Control')
  await app.page.waitForSelector('[role="dialog"] input', { visible: true })
  await app.page.locator('[role="dialog"] input').fill('Show videos')
  await app.page.locator('[role="option"]::-p-text(Show videos)').click()
  await path('/videos')
  visited.add('videos')
  await button(app.page, 'Photos', '[role="radiogroup"]')
  await path('/?media=photos')
  await app.page.goBack()
  s.check('a media replacement leaves the prior Unicode search in history', await app.page.evaluate(() => location.pathname === '/search' && new URLSearchParams(location.search).get('q') === '海辺 کی شام'))
  await app.page.goForward(); await path('/?media=photos')

  phase = 'settings aliases and tool handoffs'
  for (const [alias, canonical] of [['video', 'viewing'], ['compression', 'files'], ['about', 'updates'], ['ai', 'search']]) {
    await incoming(`/settings?section=${alias}`, `/settings?section=${canonical}`)
    s.check(`${alias}: incoming compatibility normalises to ${canonical}`, await app.page.evaluate(section => new URLSearchParams(location.search).get('section') === section, canonical))
  }
  await settings('files')
  await app.page.locator('a[href="/settings/organise"]').click()
  await path('/settings/organise'); await header('Organise files on disk')
  visited.add('organise')
  await settings('privacy')
  await app.page.locator('a[href="/settings/blocked"]').click()
  await path('/settings/blocked')
  visited.add('blocked')
  await app.page.waitForSelector('input[type="password"]', { visible: true })
  s.check('the Settings handoff retains the PIN gate', !!(await app.page.$('input[type="password"]')))

  phase = 'privacy and navigation-away relock'
  await lib(app.page, 'SetBlock', 'asset', first, '', 'photos')
  await app.page.reload()
  await unlock()
  await app.page.waitForSelector('[data-blocked="show"]', { visible: true })
  await app.page.waitForSelector('[data-block-cover] img', { visible: true })
  const protectedURL = await app.page.$eval('[data-block-cover] img', img => img.src)
  s.check('an unlocked protected thumbnail is served before navigation', await app.page.evaluate(async url => (await fetch(url)).status, protectedURL) === 200)
  await sidebar('/things')
  let locked = false
  await until(async () => { try { await lib(app.page, 'Blocks'); return false } catch (error) { locked = String(error).includes('blocks_locked'); return locked } }, 'navigation did not relock the protected service')
  const status = await app.page.evaluate(async url => (await fetch(url)).status, protectedURL)
  s.check('leaving Blocked relocks the service and denies its previously served media', locked && [403, 404].includes(status), { locked, status })
  await settings('privacy'); await app.page.locator('a[href="/settings/blocked"]').click(); await unlock()
  s.check('unlocking deliberately restores protected media for this screen', await app.page.evaluate(async url => (await fetch(url)).status, protectedURL) === 200)
  await incoming('/things')
  await header('Things')
  let reloadLocked = false
  try { await lib(app.page, 'Blocks') } catch (error) { reloadLocked = String(error).includes('blocks_locked') }
  const reloadStatus = await app.page.evaluate(async url => (await fetch(url)).status, protectedURL)
  s.check('a full incoming-page reload also ends the old PIN/media session', reloadLocked && [403, 404].includes(reloadStatus), { reloadLocked, reloadStatus })
  await settings('privacy'); await app.page.locator('a[href="/settings/blocked"]').click(); await unlock()
  await app.page.locator('[data-blocked="unblock"]').click()
  await until(async () => (await lib(app.page, 'Selection', { filter: '' }, [], false)).ids.includes(first), 'unblocking did not restore the original asset')
  s.check('unblocking through the protected control restores the original asset', true)

  phase = 'malformed incoming locations'
  trace = await mutationTrace(app.page, undefined, new Map(['Timeline', 'Days', 'Selection'].map(name => [bindingID('libraryservice', name), name])))
  for (const url of ['/people/0', '/people/1.5', '/people/9007199254740992', '/things/nope', '/albums/0', '/places/-1', '/events/bad.id', '/search?place=0', '/search?q=one&q=two', '/settings?section=unknown']) {
    trace.reset()
    await incoming(url)
    await until(() => app.page.evaluate(() => document.body.innerText.includes('This location is unavailable')), 'malformed input mounted a feature')
    await trace.idle()
    s.check(`${url}: malformed input shows no grid and starts no whole-library request`, !(await app.page.$('.scroller')) && !trace.calls.some(call => ['Timeline', 'Days', 'Selection', 'Hits'].includes(call.name)))
  }
  await incoming('/not-a-destination', '/')
  s.check('only intentional unknown paths fall back to Library', !!(await app.page.waitForSelector('.scroller', { visible: true })))
  await incoming(`/people/999999`)
  s.check('a stale safe ID remains its own location rather than opening all photos', await app.page.evaluate(() => location.pathname === '/people/999999') && (await lib(app.page, 'Selection', { filter: '', person: 999999 }, [], false)).ids.length === 0)
  s.check('malformed-input instrumentation has no errors', trace.errors.length === 0, trace.errors)

  phase = 'themes, drawer and startup'
  for (const theme of ['light', 'dark']) {
    await lib(app.page, 'SetPreferences', { ...await lib(app.page, 'Preferences'), theme })
    await incoming('/settings?section=appearance')
    await until(() => app.page.evaluate(theme => document.documentElement.classList.contains('dark') === (theme === 'dark'), theme), 'the selected theme did not apply')
    s.check(`${theme}: the typed settings destination retains the selected appearance`, await app.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  }
  await resize(app.page, 700, 800)
  await app.page.locator('button[aria-label="Open navigation"]').click()
  await app.page.locator('[role="dialog"] a[href="/albums"]').click()
  await path('/albums')
  await app.page.waitForSelector('[role="dialog"] a[href="/albums"]', { hidden: true })
  s.check('narrow navigation dismisses its drawer after the destination changes', await app.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  await app.page.screenshot({ path: join(s.out, 'narrow-navigation.png') })
  await lib(app.page, 'SetPreferences', { ...await lib(app.page, 'Preferences'), startView: 'albums' })
  await stop(); await start()
  await path('/albums'); await header('Albums')
  s.check('the persisted startup choice replaces initial Library with Albums', await app.page.evaluate(() => location.pathname === '/albums'))
  await app.page.screenshot({ path: join(s.out, 'startup-albums.png') })
  const expected = ['library', 'favourites', 'videos', 'trash', 'people', 'person', 'personFaces', 'things', 'thing', 'albums', 'album', 'places', 'place', 'events', 'event', 'duplicates', 'compression', 'search', 'settings', 'blocked', 'organise']
  s.check('every production destination was exercised independently of the contract table', expected.every(name => visited.has(name)) && visited.size === expected.length, [...visited])
  s.check('no external requests', external.length === 0, external)
  await Promise.all(consoleReads)
  const unexpected = diagnostics.filter(row => !/custom\.js/.test(row.error) &&
    !(['privacy and navigation-away relock', 'malformed incoming locations'].includes(row.phase) && /^Failed to load resource/.test(row.error)))
  s.check('no uncaught, unexpected console or navigation errors', errors.length === 0 && unexpected.length === 0, { errors, unexpected })
} catch (error) {
  failure = { phase, stack: error.stack ?? String(error) }
  s.check(`navigation completed (${phase})`, false, String(error))
  if (app) {
    await diagnostic(app.page.screenshot({ path: join(s.out, 'failure.png') }))
    failure.dom = await diagnostic(app.page.evaluate(() => ({ path: location.pathname, text: document.body.innerText.slice(0, 18000) })))
  }
} finally {
  await stop().catch(error => { failure ??= { phase, error: String(error) } })
  await Promise.all(consoleReads)
  s.finish({ failure, visited: [...visited], errors, diagnostics })
}
