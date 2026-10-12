// Native acceptance of the unconfigured release build. Signed delivery is
// exercised in the Go fixture feed; this suite never fabricates production trust.
import { join } from 'node:path'
import { close, go, launch, resize, sleep, updates } from '../lib/app.mjs'
import { libraryCopy } from '../lib/fixtures.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('updates')
const dataDir = await libraryCopy(join(s.run, 'data'))
let app
const external = []
try {
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  const page = app.page
  s.watch(page)
  page.on('request', (request) => {
    const address = request.url()
    if (
      !address.startsWith('data:') &&
      !address.startsWith('blob:') &&
      !['localhost', '127.0.0.1', 'wails.localhost', ''].includes(new URL(address).hostname)
    )
      external.push(address)
  })
  await page.waitForSelector('.shell, .scroller', { timeout: 60000 })
  const status = await updates(page, 'Status')
  s.check('automatic discovery is off by default', status.policy.automatic === false)
  s.check('stable is the default channel', status.policy.channel === 'stable')
  s.check('public delivery is disabled without production trust', status.configured === false)
  s.check('a development binary does not infer installation ownership', status.managed === false)
  const refused = async (method, ...args) => {
    try {
      await updates(page, method, ...args)
      return false
    } catch (error) {
      return String(error).includes('update_unavailable')
    }
  }
  s.check('manual discovery fails closed before any request', await refused('Check'))
  s.check(
    'policy cannot enable unconfigured delivery',
    await refused('SetPolicy', { ...status.policy, automatic: true }),
  )
  await go(page, '/settings?section=updates', 700)
  await page.waitForSelector('[data-category="updates"]')
  const visible = await page.$eval(
    '[data-category="updates"]',
    (element) => getComputedStyle(element).display !== 'none',
  )
  s.check('availability notice destination opens the Updates category', visible)
  const text = await page.$eval('[data-category="updates"]', (element) => element.textContent)
  s.check(
    'privacy disclosure explains IP address and certificate services',
    text.includes('IP address') && text.includes('certificate services') && text.includes('No library data'),
  )
  s.check('unsupported builds explain the offline installer route', text.includes('offline installer'))
  const toggle = await page.$eval('[data-category="updates"] [role="switch"]', (element) => ({
    disabled: element.disabled,
    checked: element.getAttribute('aria-checked'),
  }))
  s.check(
    'opt-in is disabled and unchecked without delivery configuration',
    toggle.disabled && toggle.checked === 'false',
  )
  for (const width of [1280, 720]) {
    await resize(page, width, 800)
    await sleep(300)
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
    )
    s.check(`Updates has no sideways overflow at ${width}px`, !overflow)
    await page.screenshot({ path: join(s.out, `updates-${width}.png`) })
  }
  await sleep(1300)
  s.check('opening Updates never enables discovery', !(await updates(page, 'Status')).policy.automatic)
  s.check('no off-device UI request', external.length === 0, external)
  s.check('no uncaught UI errors', s.appErrors().length === 0, s.appErrors())
} catch (error) {
  s.check('suite completed', false, String(error))
} finally {
  if (app) await close(app)
  s.finish()
}
