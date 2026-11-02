// Collection recovery and event-name history through the native app. Every
// file and setting belongs to this suite's generated scratch library.
import { execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { close, go, launch, lib, resize, search, unthrottle, waitIndexed } from '../lib/app.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('collection-recovery')
const dataDir = join(s.run, 'data')
const photos = join(s.run, 'photos')
const PIN = '724691'
mkdirSync(photos, { recursive: true })
let app
const requests = []
function watch(page) {
  s.watch(page)
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname))
      requests.push(request.url())
  })
}
async function button(page, text) {
  const selector = `button::-p-text(${text})`
  await page.waitForSelector(selector, { visible: true })
  await page.click(selector)
}
async function recovery(page, path, label) {
  await go(page, path)
  await page.waitForFunction(() =>
    [...document.querySelectorAll('main a')].some((el) => el.textContent.trim() === 'Manage blocked items'),
  )
  const link = await page.evaluateHandle(() =>
    [...document.querySelectorAll('main a')].find((el) => el.textContent.trim() === 'Manage blocked items'),
  )
  const element = link.asElement()
  if (!element) throw new Error(`missing recovery link on ${path}`)
  await element.focus()
  await page.keyboard.press('Enter')
  await page.waitForFunction(
    () => location.pathname !== '/things' && location.pathname !== '/places' && location.pathname !== '/events',
  )
  const destination = await page.evaluate(() => location.pathname)
  s.check(`${label}: keyboard recovery reaches Blocked`, destination === '/settings/blocked', destination)
  await link.dispose()
  if (destination !== '/settings/blocked') return false
  await page.waitForSelector('input[type="password"]', { visible: true })
  s.check(`${label}: recovery still asks for the PIN`, !!(await page.$('input[type="password"]')))
  await page.screenshot({ path: join(s.out, `${label.replaceAll(' ', '-')}.png`) })
  return true
}
async function undo(page) {
  await button(page, 'Undo')
}
async function rename(page, name) {
  await button(page, 'Rename…')
  await page.waitForSelector('input[aria-label="Collection name"]', { visible: true })
  await page.focus('input[aria-label="Collection name"]')
  await page.keyboard.down('Control')
  await page.keyboard.press('A')
  await page.keyboard.up('Control')
  await page.keyboard.type(name)
  await page.keyboard.press('Enter')
  await page.waitForFunction((name) => document.querySelector('h1')?.textContent === name, {}, name)
}
async function collection(page, path) {
  try {
    await page.waitForSelector('[data-collection]')
  } catch (e) {
    await page.screenshot({ path: join(s.out, 'collection-failure.png') })
    throw new Error(`${path}: ${await page.$eval('main', (el) => el.innerText.slice(-1800))}; ${String(e)}`)
  }
}
try {
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await lib(app.page, 'AddSources', [photos])
  await lib(app.page, 'CreateBlockPIN', PIN)
  await app.page.reload()
  await app.page.waitForSelector('aside.sidebar')

  await s.guard('empty', async () => {
    for (const width of [1440, 800]) {
      for (const theme of ['light', 'dark']) {
        const prefs = await lib(app.page, 'Preferences')
        await lib(app.page, 'SetPreferences', { ...prefs, theme })
        await app.page.reload()
        await resize(app.page, width, 900)
        for (const path of ['/things', '/places', '/events'])
          await recovery(app.page, path, `${path.slice(1)} ${width} ${theme}`)
      }
    }
  })

  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', photos, '12'], {
    stdio: 'inherit',
  })
  await lib(app.page, 'Rescan')
  await waitIndexed(app.page, { ai: true, atLeast: 12 })
  await lib(app.page, 'SetPaused', true)
  const buckets = await lib(app.page, 'Timeline', { filter: '' })
  const ids = []
  for (const bucket of buckets)
    for (const row of await lib(app.page, 'Days', bucket.day, bucket.day, { filter: '' }))
      ids.push(...row.items.map((item) => item.id))
  s.must('all generated photos are indexed', ids.length === 12, ids)
  await lib(app.page, 'SetTakenAt', ids, Date.UTC(2026, 9, 3, 12) / 1000)
  await lib(app.page, 'SetLocation', ids, 31.52, 74.36)
  await search(app.page, 'TagAsset', ids[0], 'Recovery fixture')

  await s.guard('blocked', async () => {
    const things = (await search(app.page, 'Things')).filter((thing) => thing.kind !== 'builtin' || thing.count > 0)
    const place = (await lib(app.page, 'Places'))[0]
    const event = (await lib(app.page, 'Events'))[0]
    s.must('generated media supplies all three collection kinds', things.length && place && event, {
      things: things.map(({ id, name, count }) => ({ id, name, count })),
      place,
      event,
    })
    for (const [path, kind, targets] of [
      ['/things', 'tag', things.map((item) => item.id)],
      ['/places', 'place', [place.id]],
      ['/events', 'event', [event.id]],
    ]) {
      await go(app.page, path)
      await collection(app.page, path)
      const blocks = []
      for (const id of targets) blocks.push((await lib(app.page, 'SetBlock', kind, id, '', 'label')).id)
      await app.page.waitForSelector('[data-collection]', { hidden: true })
      if (!(await recovery(app.page, path, `last ${kind} blocked`))) continue
      await app.page.focus('input[type="password"]')
      await app.page.type('input[type="password"]', PIN)
      await app.page.keyboard.press('Enter')
      await app.page.waitForFunction(() => document.querySelector('[data-block-row], [role="alert"]'))
      const unlocked = !!(await app.page.$('[data-block-row]'))
      s.must(
        `${kind}: the PIN opens the blocked list`,
        unlocked,
        unlocked ? kind : await app.page.$eval('main', (el) => el.innerText.slice(-1200)),
      )
      for (const id of blocks) {
        const unblock = `[data-block-row="${id}"] [data-blocked="unblock"]`
        await app.page.waitForSelector(unblock, { visible: true })
        await app.page.click(unblock)
        await app.page.waitForSelector(`[data-block-row="${id}"]`, { hidden: true })
      }
      await go(app.page, path)
      await collection(app.page, path)
      s.check(`${kind}: unblocking returns the collection`, !!(await app.page.$('[data-collection]')))
      let locked = false
      try {
        await lib(app.page, 'Blocks')
      } catch {
        locked = true
      }
      s.check(`${kind}: leaving recovery locks the engine gate`, locked)
    }
  })

  // Remove any fixture blocks left by a deliberately failing baseline run.
  await lib(app.page, 'UnlockBlocks', PIN)
  for (const block of (await lib(app.page, 'Blocks')) ?? []) await lib(app.page, 'RemoveBlock', block.id)
  await lib(app.page, 'LockBlocks')
  const event = (await lib(app.page, 'Events'))[0]
  await s.guard('names', async () => {
    await go(app.page, `/events/${event.id}`)
    await app.page.waitForSelector('.scroller .cell')
    await rename(app.page, 'A temporary title')
    await undo(app.page)
    await app.page.waitForFunction(() => !document.querySelector('h1')?.textContent.includes('A temporary title'))
    const restored = await lib(app.page, 'Event', event.id)
    s.check('Undo restores automatic naming rather than storing the displayed name', !restored.nameOverride, restored)
    await lib(app.page, 'SetLocation', ids, 48.86, 2.35)
    const corrected = await lib(app.page, 'Event', event.id)
    s.check(
      'an undone automatic name follows a later place correction',
      !corrected.nameOverride && corrected.name !== event.name,
      corrected,
    )

    await app.page.reload()
    await rename(app.page, 'Our trip')
    await rename(app.page, 'Another title')
    await undo(app.page)
    await app.page.waitForFunction(() => document.querySelector('h1')?.textContent === 'Our trip')
    s.check(
      'Undo of a rename restores the custom override',
      (await lib(app.page, 'Event', event.id)).nameOverride === 'Our trip',
    )
    await button(app.page, 'Reset name')
    await app.page.waitForFunction(() => document.querySelector('h1')?.textContent !== 'Our trip')
    await undo(app.page)
    await app.page.waitForFunction(() => document.querySelector('h1')?.textContent === 'Our trip')
    s.check(
      'Undo of Reset name restores the custom override',
      (await lib(app.page, 'Event', event.id)).nameOverride === 'Our trip',
    )
    await button(app.page, 'Reset name')
    await app.page.waitForFunction(() => document.querySelector('h1')?.textContent !== 'Our trip')
  })

  for (const place of await lib(app.page, 'Places')) await lib(app.page, 'SetBlock', 'place', place.id, '', 'label')
  await go(app.page, '/settings?section=privacy')
  await close(app)
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await s.guard('restart', async () => {
    const restarted = await lib(app.page, 'Event', event.id)
    s.check('automatic naming survives an application restart', !restarted.nameOverride, restarted)
    await recovery(app.page, '/places', 'restart recovery')
    await go(app.page, '/settings/blocked')
    await app.page.waitForSelector('input[type="password"]', { visible: true })
    s.check('the PIN remains configured after restart', await lib(app.page, 'BlockPINConfigured'))
  })
} catch (e) {
  s.check('the workflows ran to the end', false, String(e))
  await app?.page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  if (app) await close(app)
  s.check('no request left the PC', requests.length === 0, requests)
  s.check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors())
  s.finish()
}
