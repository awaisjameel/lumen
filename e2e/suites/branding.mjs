// The Lumen mark wherever it appears (Welcome, sidebar, collapsed rail,
// About, the narrow header and its drawer), in both themes, and the favicon.
//   node suites/branding.mjs
import { copyFileSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { close, launch, lib, resize, sleep } from '../lib/app.mjs'
import { fixture } from '../lib/fixtures.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('branding')
const { check } = s
const root = s.out
// One photo is enough to leave the Welcome screen for the app shell.
const source = join(s.run, 'source')
mkdirSync(source)
copyFileSync(join(fixture('media'), 'photo-01.jpg'), join(source, 'brand-photo.jpg'))
const app = await launch({ exe: s.exe, dataDir: join(s.run, 'data'), port: s.port })
const page = app.page
s.watch(page)
const shot = async (name) => {
  await sleep(350)
  return page.screenshot({ path: join(root, name + '.png') })
}
const prefs = async (patch) => {
  const p = await lib(page, 'Preferences')
  await lib(page, 'SetPreferences', { ...p, ...patch })
  await page.reload()
  await sleep(800)
}
const mark = (selector) =>
  page.$eval(selector, (e) => ({
    loaded: e.complete && e.naturalWidth === 128,
    w: e.getBoundingClientRect().width,
    h: e.getBoundingClientRect().height,
    alt: e.alt,
    src: e.getAttribute('src'),
  }))
try {
  await resize(page, 1280, 820)
  await page.waitForSelector('.welcome .logo')
  for (const theme of ['light', 'dark']) {
    await prefs({ theme })
    await page.waitForSelector('.welcome .logo')
    const m = await mark('.welcome .logo')
    check(`${theme} onboarding logo`, m.loaded && m.w === 64 && m.h === 64 && m.alt === '', m)
    check(
      `${theme} persisted theme`,
      await page.evaluate((theme) => document.documentElement.classList.contains('dark') === (theme === 'dark'), theme),
    )
    await shot(`welcome-${theme}`)
  }
  const favicon = await page.evaluate(async () => {
    const link = document.querySelector('link[rel="icon"]')
    const r = await fetch(link.href)
    const img = new Image()
    img.src = URL.createObjectURL(await r.blob())
    await img.decode()
    const canvas = document.createElement('canvas')
    canvas.width = 32
    canvas.height = 32
    const ctx = canvas.getContext('2d')
    ctx.drawImage(img, 0, 0)
    const alpha = ctx.getImageData(0, 0, 1, 1).data[3]
    return { status: r.status, w: img.naturalWidth, h: img.naturalHeight, alpha, type: link.type }
  })
  check(
    'favicon is the dedicated transparent 32px PNG',
    favicon.status === 200 &&
      favicon.w === 32 &&
      favicon.h === 32 &&
      favicon.alpha === 0 &&
      favicon.type === 'image/png',
    favicon,
  )
  await lib(page, 'SetPauseBusy', false)
  await lib(page, 'SetIdleOnly', false)
  await lib(page, 'AddSources', [source])
  for (let i = 0; i < 80; i++) {
    const p = await lib(page, 'Progress')
    if (p.total && !p.scanning && !p.metadata && !p.thumbnail) break
    await sleep(250)
  }
  await lib(page, 'SetPaused', true)
  for (const theme of ['light', 'dark']) {
    await resize(page, 1440, 900)
    await prefs({ theme, sidebarCollapsed: false })
    await page.waitForSelector('.app-nav .brand-mark')
    const m = await mark('.app-nav .brand-mark')
    check(`${theme} expanded sidebar logo`, m.loaded && m.w === 24 && m.h === 24 && m.alt === '', m)
    await shot(`sidebar-${theme}`)
    await page.keyboard.down('Control')
    await page.keyboard.press('b')
    await page.keyboard.up('Control')
    await sleep(450)
    const rail = await mark('.app-nav.rail .brand-mark')
    check(`${theme} collapsed rail has an accessible Lumen mark`, rail.loaded && rail.alt === 'Lumen', rail)
    await shot(`rail-${theme}`)
    await prefs({ theme, sidebarCollapsed: false })
    await page.click('.app-nav a[href="/settings"]')
    await page.waitForSelector('.about-brand .brand-mark')
    const button = await page.evaluateHandle(() =>
      [...document.querySelectorAll('.category-nav button')].find((e) => e.textContent.trim() === 'Updates and about'),
    )
    await button.asElement().click()
    await sleep(250)
    const about = await mark('.about-brand .brand-mark')
    check(
      `${theme} About uses the shared brand`,
      about.loaded && about.w === 40 && about.h === 40 && about.alt === '',
      about,
    )
    await shot(`about-${theme}`)
    // Below 720 px the sidebar becomes a drawer behind a narrow header.
    await resize(page, 700, 780)
    await page.waitForSelector('.narrow-bar .brand-mark')
    const narrow = await mark('.narrow-bar .brand-mark')
    check(`${theme} narrow header logo`, narrow.loaded && narrow.w === 20 && narrow.h === 20, narrow)
    await shot(`narrow-${theme}`)
    await page.click('.narrow-bar button')
    await page.waitForSelector('[role="dialog"] .app-nav .brand-mark')
    await sleep(350)
    const drawer = await mark('[role="dialog"] .app-nav .brand-mark')
    check(`${theme} navigation drawer uses the shared brand`, drawer.loaded && drawer.alt === '', drawer)
    check(
      `${theme} drawer mark is fully visible`,
      await page.$eval('[role="dialog"] .app-nav .brand-mark', (e) => {
        const r = e.getBoundingClientRect()
        return r.x >= 0 && r.y >= 0 && r.right <= innerWidth && r.bottom <= innerHeight
      }),
    )
    await shot(`drawer-${theme}`)
    await page.keyboard.press('Escape')
    await sleep(250)
  }
  check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors().join(' || '))
} catch (e) {
  check('branding ran to the end', false, String(e))
  await page.screenshot({ path: join(root, 'failure.png') }).catch(() => {})
} finally {
  await close(app)
  s.finish()
}
