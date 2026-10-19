// A new, empty library: Lumen opens on the Welcome screen, offers folders,
// and adds none by itself. The suite only looks; it never presses Start.
//   node suites/onboarding.mjs
import { join } from 'node:path'
import { close, launch, lib, resize, sleep } from '../lib/app.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('onboarding')
const { check, guard } = s
const app = await launch({ exe: s.exe, dataDir: join(s.run, 'data'), port: s.port })
const { page } = app
s.watch(page)
await guard('welcome', async () => {
  await resize(page, 1280, 820)
  await page.waitForFunction(() => document.body.innerText.includes('Which folders hold your photos and videos?'), {
    timeout: 20000,
  })
  const state = await page.evaluate(() => ({
    h1: document.querySelector('h1')?.textContent?.trim(),
    buttons: [...document.querySelectorAll('button')]
      .map((b) => b.textContent.trim() || b.getAttribute('aria-label'))
      .filter(Boolean),
    shell: !!document.querySelector('aside.sidebar'),
    overflow: document.documentElement.scrollWidth - innerWidth,
  }))
  check('an empty library opens on the Welcome screen, without the app shell', !!state.h1 && !state.shell, state)
  check('the Welcome screen offers its choices as buttons', state.buttons.length > 0, state.buttons)
  check('the Welcome screen fits the window', state.overflow <= 0, `overflow=${state.overflow}`)
  await page.screenshot({ path: join(s.out, 'welcome.png') })

  await resize(page, 720, 480)
  await sleep(600)
  const narrow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)
  check('the Welcome screen fits the minimum window', narrow <= 0, `overflow=${narrow}`)
  await page.screenshot({ path: join(s.out, 'welcome-minimum.png') })

  check('no folder was added by opening Lumen', ((await lib(page, 'Sources')) ?? []).length === 0)
  const progress = await lib(page, 'Progress')
  check('nothing is indexed before the user chooses', progress.total === 0 && !progress.scanning, progress)
})
check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors().join(' || '))
await close(app)
s.finish()
