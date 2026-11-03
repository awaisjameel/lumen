// Recent searches through the real controls and persisted library. Ordering
// faults are covered deterministically by the adjacent frontend regressions.
import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { close, go, launch, lib, resize, search } from '../lib/app.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('search-history')
const dataDir = join(s.run, 'data')
const photos = join(s.run, 'photos')
mkdirSync(photos, { recursive: true })
let app
const requests = []

function watch(page) {
  s.watch(page)
  page.on('pageerror', (error) => console.log(`PAGE ERROR ${error.stack ?? error}`))
  page.on('console', (message) => {
    if (message.type() === 'error') console.log(`CONSOLE ERROR ${message.text()} ${JSON.stringify(message.location())}`)
  })
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname))
      requests.push(request.url())
  })
}

async function history(page) {
  await page.waitForSelector('input[role="combobox"]', { visible: true })
  await page.focus('input[role="combobox"]')
  await page.waitForFunction(() => !!document.querySelector('[data-search-forget]'))
  return page.$$eval('[role="option"]', (rows) => rows.map((row) => row.textContent.trim()))
}

try {
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await lib(app.page, 'AddSources', [photos])
  await search(app.page, 'RememberSearch', 'old search')
  await close(app)
  app = undefined
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await resize(app.page, 1440, 900)
  s.must('the empty search box offers persisted history', (await history(app.page)).includes('old search'))
  await app.page.click('[data-search-forget]')
  await app.page.waitForFunction(() => !document.querySelector('[data-search-forget]'))
  s.must(
    'clearing through the control persists an empty history',
    (await search(app.page, 'RecentSearches')).length === 0,
  )

  await app.page.type('input[role="combobox"]', 'new search')
  await app.page.keyboard.press('Enter')
  await app.page.waitForFunction(() => location.pathname === '/search' && location.search.includes('new+search'))
  await go(app.page, '/things')
  await go(app.page, '/')
  const current = await history(app.page)
  s.must(
    'leaving and reopening offers the new query without the cleared query',
    current.includes('new search') && !current.includes('old search'),
    current,
  )
  await close(app)
  app = undefined

  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  const persisted = await search(app.page, 'RecentSearches')
  s.must(
    'only the new search survives a restart',
    JSON.stringify(persisted) === JSON.stringify(['new search']),
    persisted,
  )
  s.must('reopened controls show the persisted query', (await history(app.page)).includes('new search'))
  await app.page.screenshot({ path: join(s.out, 'restarted-history.png') })
  s.check('no external network requests', requests.length === 0, requests)
  s.check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors())
} catch (error) {
  s.check('the history workflow ran to the end', false, String(error))
  if (app) await app.page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  if (app) await close(app)
  s.finish()
}
