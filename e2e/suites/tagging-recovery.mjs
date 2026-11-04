// A real annotation commit and a refused OCR queue remain separate outcomes.
import { execFileSync } from 'node:child_process'
import { join } from 'node:path'
import { close, launch, lib, resize, unthrottle, waitIndexed } from '../lib/app.mjs'
import { button, until } from '../lib/controls.mjs'
import { bindingID } from '../lib/mutation-trace.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('tagging-recovery')
const dataDir = join(s.run, 'data')
const photos = join(s.run, 'photos')
const db = join(dataDir, 'library.db')
const helper = join(E2E, 'fixtures', 'tagging.py')
const tagMethod = bindingID('searchservice', 'TagAsset')
const observations = []
const external = []
let app
let phase = 'fixture'
let asset
let assets
let failure
function fixture(action, id = asset) {
  const output = execFileSync(process.env.PYTHON ?? 'python', [helper, action, action === 'document' ? photos : db, String(id ?? 0)],
    { encoding: 'utf8' })
  return action === 'state' ? JSON.parse(output) : undefined
}
function watch(page) {
  s.watch(page)
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname)) external.push(request.url())
  })
}
async function restart() {
  if (app) await close(app)
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await lib(app.page, 'SetPaused', true)
  await resize(app.page, 1440, 1000)
  await app.page.waitForSelector('.scroller .cell')
}
async function selectDocument(page) {
  await page.locator(`#cell-${asset}`).click()
  const details = 'button[aria-label="Details"]'
  if ((await page.$eval(details, (el) => el.getAttribute('aria-pressed'))) !== 'true') await page.locator(details).click()
  await page.waitForSelector('.inspector-body [data-edit-date]')
  const name = (await lib(page, 'Asset', asset)).paths[0].split(/[\\/]/).at(-1)
  await until(() => page.$eval('.preview-caption h3', (el, name) => el.textContent.trim() === name, name),
    'the inspector did not settle on the document')
}
async function submitTag(page) {
  const response = page.waitForResponse((reply) => {
    if (new URL(reply.url()).pathname !== '/wails/runtime') return false
    const body = JSON.parse(reply.request().postData() ?? 'null')
    return body?.args?.methodID === tagMethod
  })
  await button(page, 'Add tag', '[role="dialog"]')
  const reply = await response
  await until(() => page.evaluate(() => {
    const save = [...document.querySelectorAll('[role="dialog"] button')].find((el) => el.textContent.trim() === 'Add tag')
    return !save || !save.disabled
  }), 'tag save did not settle')
  observations.push({ phase, status: reply.status(), response: await reply.text(), persisted: fixture('state'),
    ui: await page.evaluate(() => ({
      draft: document.querySelector('[data-tag-draft]')?.value,
      error: document.querySelector('[role="dialog"] [role="alert"]')?.textContent,
      tags: document.querySelector('.inspector-body')?.textContent,
    })),
  })
  return reply.status()
}
try {
  fixture('document')
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await unthrottle(app.page)
  await lib(app.page, 'AddSources', [photos])
  await waitIndexed(app.page, { ai: true, atLeast: 2, timeout: 180_000, log: console.log })
  await lib(app.page, 'SetPaused', true)
  assets = (await lib(app.page, 'Selection', { filter: '' }, [], false)).ids
  asset = assets[0]
  s.must('two distinct synthetic documents are indexed', assets.length === 2 && assets.every(Number.isInteger), assets)
  await close(app)
  app = undefined
  fixture('prepare')
  await restart()
  await lib(app.page, 'SetOCR', true)
  s.must('the fixture has no OCR job before manual eligibility', fixture('state').ocr.length === 0)

  phase = 'pre-commit failure'
  fixture('refuse-tag')
  await selectDocument(app.page)
  await button(app.page, 'Add a tag…', '.inspector-body')
  await app.page.locator('[data-tag-draft]').fill('Screenshots')
  const rejected = await submitTag(app.page)
  s.check('a refused annotation remains a failed write', rejected >= 400 && fixture('state').tags.length === 0, observations.at(-1))
  s.check('a genuine write failure preserves the draft and editor', await app.page.$eval('[data-tag-draft]', (el) => el.value === 'Screenshots'))
  fixture('allow-tag')

  phase = 'committed tag and refused queue'
  fixture('refuse-queue')
  const saved = await submitTag(app.page)
  const persisted = fixture('state')
  s.must('the annotation is committed independently of the refused queue', persisted.tags.some(([name, source]) => name === 'Screenshots' && source === 'user') && persisted.ocr.length === 0, persisted)
  s.must('a committed annotation is presented as a successful save', saved === 200, observations.at(-1))
  await until(() => app.page.evaluate(() => !document.querySelector('[data-tag-draft]')), 'the committed editor did not close')
  s.check('the details list reloads the committed annotation immediately', await app.page.$eval('.inspector-body', (el) => el.innerText.includes('Screenshots')))
  await until(async () => (await lib(app.page, 'Progress')).ocrQueueRetry === true, 'the failed follow-up was not retained for retry')
  await app.page.locator('[data-activity-trigger]').click()
  await app.page.waitForSelector('[data-activity-details]', { visible: true })
  // Progress events are deliberately coalesced; the direct RPC snapshot can
  // precede the sidebar's next event. Wait for the real rendered feedback.
  const feedbackStarted = Date.now()
  await until(() => app.page.$eval('[data-activity-details]', (el) => el.innerText.includes('Text reading will retry')),
    'activity did not receive the pending scheduling recovery', 10_000)
  s.check('activity explains that saved tags are safe and text reading will retry', await app.page.$eval('[data-ocr-retry]', (el) => el.innerText.includes('Your tags are saved.')),
    { millisecondsAfterOpening: Date.now() - feedbackStarted })
  await app.page.screenshot({ path: join(s.out, 'committed-tag-retry.png') })
  await app.page.keyboard.press('Escape')

  phase = 'in-session scheduling recovery'
  fixture('allow-queue')
  await until(() => fixture('state').ocr.length === 1, 'the in-session retry did not queue persisted eligibility')
  await until(async () => !(await lib(app.page, 'Progress')).ocrQueueRetry, 'in-session recovery did not clear retry feedback')
  await until(() => app.page.$eval('[data-activity-trigger]', (el) => !el.innerText.includes('Text reading will retry')),
    'activity did not clear the recovered scheduling feedback', 10_000)
  s.check('activity clears retry feedback after scheduling recovers', await app.page.$eval('[data-activity-trigger]', (el) => !el.innerText.includes('Text reading will retry')))
  s.check('scheduling recovers independently of paused processing', (await lib(app.page, 'Progress')).paused && fixture('state').text === '')
  s.check('in-session recovery preserves exactly one user annotation', fixture('state').tags.filter(([name, source]) => name === 'Screenshots' && source === 'user').length === 1)

  phase = 'a second committed tag and refused queue'
  asset = assets[1]
  fixture('refuse-queue')
  await selectDocument(app.page)
  await button(app.page, 'Add a tag…', '.inspector-body')
  await app.page.locator('[data-tag-draft]').fill('Screenshots')
  s.must('another committed annotation remains a successful save', await submitTag(app.page) === 200, observations.at(-1))
  await until(() => app.page.evaluate(() => !document.querySelector('[data-tag-draft]')), 'the second committed editor did not close')
  await until(async () => (await lib(app.page, 'Progress')).ocrQueueRetry === true, 'the second follow-up was not retained')

  phase = 'restart with queue fault still present'
  await restart()
  s.check('restart preserves exactly one user annotation', fixture('state').tags.filter(([name, source]) => name === 'Screenshots' && source === 'user').length === 1)
  await until(async () => (await lib(app.page, 'Progress')).ocrQueueRetry === true, 'startup did not retain its failed reconciliation')
  fixture('allow-queue')
  await until(() => fixture('state').ocr.length === 1, 'the owned retry did not queue persisted eligibility', 30_000)
  await until(async () => !(await lib(app.page, 'Progress')).ocrQueueRetry, 'successful scheduling did not clear retry feedback')
  s.check('scheduling recovers while processing remains paused', (await lib(app.page, 'Progress')).paused && fixture('state').text === '')
  await lib(app.page, 'SetPaused', false)
  await until(() => assets.every((id) => fixture('state', id).text.toLowerCase().includes('invoice')), 'the recovered OCR jobs did not persist both documents’ text', 120_000)
  s.check('both recovery paths preserve exactly one user annotation per photo', assets.every((id) => fixture('state', id).tags.filter(([name, source]) => name === 'Screenshots' && source === 'user').length === 1))
  await restart()
  s.check('both annotations and independently recovered text survive restart', assets.every((id) => {
    const state = fixture('state', id)
    return state.tags.some(([name, source]) => name === 'Screenshots' && source === 'user') && state.text.toLowerCase().includes('invoice')
  }))
  s.check('no external requests', external.length === 0, external)
  s.check('no unexpected frontend errors', s.appErrors().length === 0, s.appErrors())
} catch (error) {
  failure = { phase, stack: error.stack ?? String(error) }
  s.check(`tagging recovery completed (${phase})`, false, String(error))
  if (app) await app.page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  if (app) await close(app)
  s.finish({ observations, failure })
}
