// The development-only component showcase, in a real development run
// (`wails3 dev`) on a scratch library: it renders every section and icon and
// switches theme. Opt-in: it needs the toolchain (source scripts/env.sh, with
// CGO_ENABLED=1 and CC set as for a build) and builds the app in dev mode.
//   node suites/showcase.mjs
import { execFileSync, spawn } from 'node:child_process'
import { join } from 'node:path'
import puppeteer from 'puppeteer-core'
import { go, sleep } from '../lib/app.mjs'
import { libraryCopy } from '../lib/fixtures.mjs'
import { ROOT, suite } from '../lib/suite.mjs'

const s = suite('showcase')
const { check } = s
const dataDir = await libraryCopy(join(s.run, 'data'))
const dev = spawn('wails3', ['dev', '-config', './build/config.yml', '-port', '9245'], {
  cwd: ROOT,
  env: { ...process.env, LUMEN_DATA_DIR: dataDir, LUMEN_DEVTOOLS_PORT: String(s.port) },
  stdio: 'ignore',
  shell: true,
})
let browser
try {
  for (let i = 0; i < 600 && !browser; i++) {
    try {
      browser = await puppeteer.connect({ browserURL: `http://127.0.0.1:${s.port}`, defaultViewport: null })
    } catch {
      await sleep(500)
    }
  }
  if (!browser) throw new Error('the development app did not start')
  let page
  for (let i = 0; i < 100 && !page; i++) {
    page = (await browser.pages()).find((p) => p.url().startsWith('http'))
    if (!page) await sleep(200)
  }
  s.watch(page)
  await page.reload()
  await sleep(8000)
  await go(page, '/__showcase', 5000)
  await page.waitForFunction(
    () => [...document.querySelectorAll('h1')].some((h) => h.textContent.includes('Component showcase')),
    { timeout: 15000 },
  )
  await sleep(1500)
  await page.screenshot({ path: join(s.out, 'showcase-light.png') })
  const before = await page.evaluate(() => ({
    sections: [...document.querySelectorAll('.frame section h2')].map((h) => h.textContent.trim()),
    icons: document.querySelectorAll('.icons svg').length,
    emptyIcons: [...document.querySelectorAll('.icons svg')].filter((svg) => !svg.innerHTML.trim()).length,
    dark: document.documentElement.classList.contains('dark'),
    dev: !!document.querySelector('script[src*="@vite"]'),
  }))
  check('the showcase is served by the development server', before.dev)
  check('the showcase renders its sections', before.sections.length > 0, before.sections)
  check(
    'every icon of the set is drawn',
    before.icons > 0 && before.emptyIcons === 0,
    `${before.icons} icons, ${before.emptyIcons} empty`,
  )
  // Dark through the showcase's own theme control.
  const dark = await page.evaluateHandle(
    () =>
      [...document.querySelectorAll('[role="radio"]')].find(
        (r) => r.getAttribute('value') === 'dark' || r.closest('label,div')?.textContent?.trim() === 'dark',
      ) ?? null,
  )
  await dark.asElement()?.click()
  await sleep(800)
  check(
    "the showcase's theme control switches to dark",
    !before.dark && (await page.evaluate(() => document.documentElement.classList.contains('dark'))),
  )
  await page.screenshot({ path: join(s.out, 'showcase-dark.png') })
  check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors().join(' || '))
} catch (e) {
  check('the showcase ran to the end', false, String(e))
} finally {
  try {
    await browser?.disconnect()
  } catch {
    /* gone */
  }
  // Only this run's own process tree: never another Lumen on the PC.
  for (const force of [[], ['/F']]) {
    try {
      execFileSync('taskkill', ['/PID', String(dev.pid), '/T', ...force], { stdio: 'ignore' })
    } catch {
      /* gone */
    }
    await sleep(4000)
  }
  s.finish()
}
