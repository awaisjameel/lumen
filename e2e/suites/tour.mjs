// Tours every route of the build on the indexed base library and records, per
// screen: a screenshot, horizontal overflow, console errors and every request
// the WebView made. External requests are refused (the offline check).
//   node suites/tour.mjs
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { close, go, launch, lib, people, resize, search, sleep } from '../lib/app.mjs'
import { libraryCopy } from '../lib/fixtures.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('tour')
const { check } = s
const app = await launch({ exe: s.exe, dataDir: await libraryCopy(join(s.run, 'data')), port: s.port })
const { page } = app
const report = { screens: [], requests: { local: 0, externalBlocked: [] }, consoleErrors: [], checks: {} }

page.on('response', async (r) => {
  if (r.status() < 400) return
  // A read the page cancels on leaving a screen (a card's preview) is answered
  // 422 "context canceled", as the bridge's isCancelled expects: not an error.
  if (r.status() === 422 && /"message":"context canceled"/.test(await r.text().catch(() => ''))) return
  report.consoleErrors.push(`${r.status()} ${r.url()}`)
})

// Every request: local ones go on, anything else is refused and recorded.
const session = await page.createCDPSession()
await session.send('Fetch.enable', { patterns: [{ urlPattern: '*' }] })
session.on('Fetch.requestPaused', async (e) => {
  const url = e.request.url
  const local = /^(https?:\/\/(wails\.localhost|127\.0\.0\.1|localhost)(:\d+)?\/|data:|blob:|about:)/.test(url)
  try {
    if (local) {
      report.requests.local++
      await session.send('Fetch.continueRequest', { requestId: e.requestId })
    } else {
      report.requests.externalBlocked.push(url)
      await session.send('Fetch.failRequest', { requestId: e.requestId, errorReason: 'BlockedByClient' })
    }
  } catch {
    /* the page went away */
  }
})
// An error says where it was seen: the screen being visited and the script line that logged it.
let visiting = 'start'
page.on('console', (m) => {
  if (m.type() !== 'error') return
  const at = m.location()
  report.consoleErrors.push(`${m.text()} [${visiting}; ${at.url ?? ''}:${at.lineNumber ?? ''}]`)
})
page.on('pageerror', (e) => report.consoleErrors.push(`pageerror: ${String(e).slice(0, 300)}`))

// A "ResizeObserver loop" error names no culprit: keep the last observations to print with it.
await page.evaluateOnNewDocument(() => {
  const Original = window.ResizeObserver
  window.observerHistory = []
  window.ResizeObserver = class extends Original {
    constructor(cb) {
      const stack = new Error().stack
      super((entries, observer) => {
        window.observerHistory.push({
          stack,
          path: location.pathname,
          time: performance.now(),
          targets: entries.map((e) => ({
            tag: e.target.tagName,
            cls: e.target.className,
            w: e.contentRect.width,
            h: e.contentRect.height,
          })),
        })
        window.observerHistory = window.observerHistory.slice(-30)
        cb(entries, observer)
      })
    }
  }
  window.addEventListener('error', (e) => {
    if (e.message.includes('ResizeObserver'))
      console.error('OBSERVER_DIAGNOSTIC:' + JSON.stringify(window.observerHistory))
  })
})
// Reload only once the app has started: a reload under its first calls aborts
// them, and the app rightly logs a call that failed.
await page.waitForSelector('.shell', { timeout: 60000 })
await sleep(500)
await page.reload()
await sleep(2500)

async function record(name) {
  await page.screenshot({ path: join(s.out, `${name}.png`) })
  const m = await page.evaluate(() => ({
    path: location.pathname + location.search,
    overflowX: document.documentElement.scrollWidth - window.innerWidth,
    title: document.querySelector('h1')?.textContent?.trim() ?? '',
    width: window.innerWidth,
    dpr: window.devicePixelRatio,
    dark: document.documentElement.classList.contains('dark'),
  }))
  report.screens.push({ name, ...m })
  return m
}

report.checks.sources = (await lib(page, 'Sources')).length

// Collection ids for the scoped pages, where there are any.
const first = async (list) => {
  try {
    return (await list)?.[0]?.id ?? 0
  } catch {
    return 0
  }
}
const ids = {
  thing: await first(search(page, 'Things')),
  person: await first(people(page, 'People')),
  place: await first(lib(page, 'Places')),
  event: await first(lib(page, 'Events')),
  album: await first(lib(page, 'Albums')),
}
report.checks.ids = ids

const routes = [
  ['library', '/'],
  ['favourites', '/favourites'],
  ['videos', '/videos'],
  ['people', '/people'],
  ['things', '/things'],
  ['places', '/places'],
  ['events', '/events'],
  ['albums', '/albums'],
  ['duplicates', '/duplicates'],
  ['trash', '/trash'],
  ['search', '/search?q=red%20circle'],
  ['settings', '/settings'],
  ['blocked', '/settings/blocked'],
  ['organise', '/settings/organise'],
]
if (ids.thing) routes.push(['thing', `/things/${ids.thing}`])
if (ids.person) routes.push(['person', `/people/${ids.person}`], ['person-faces', `/people/${ids.person}/faces`])
if (ids.event) routes.push(['event', `/events/${ids.event}`])
if (ids.place) routes.push(['place', `/places/${ids.place}`])
if (ids.album) routes.push(['album', `/albums/${ids.album}`])

// Wide first, then narrower windows: no screen may overflow sideways at any.
const widths = [
  ['wide', 1440, 900],
  ['medium', 1040, 760],
  ['narrow', 720, 720],
  ['small', 520, 700],
]
for (const [label, w, h] of widths) {
  await resize(page, w, h)
  for (const [name, path] of routes) {
    visiting = `${label}-${name}`
    await go(page, path, 1200)
    await record(`${label}-${name}`)
  }
}
await session.send('Fetch.disable')

const overflow = report.screens.filter((x) => x.overflowX > 0).map((x) => `${x.name}:+${x.overflowX}`)
// Wails asks for an optional /wails/custom.js; its 404 is not the app's error.
const consoleErrors = report.consoleErrors.filter((e) => !/custom\.js|Failed to load resource/.test(e))
check(
  'every route was visited at every width',
  report.screens.length === routes.length * widths.length,
  `${report.screens.length} screens`,
)
check('the library has a Thing, an event and an album to open', ids.thing > 0 && ids.event > 0 && ids.album > 0, ids)
check('no screen overflows sideways', overflow.length === 0, overflow.join(' '))
check('no request left the PC', report.requests.externalBlocked.length === 0, report.requests.externalBlocked.join(' '))
check('no console or page errors', consoleErrors.length === 0, consoleErrors.slice(0, 5).join(' || '))
writeFileSync(join(s.out, 'tour.json'), JSON.stringify(report, null, 2))
await close(app)
s.finish({ localRequests: report.requests.local, ids })
