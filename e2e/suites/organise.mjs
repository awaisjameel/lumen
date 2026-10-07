// Organise on disk, from the Settings screen: preview the moves of one folder,
// confirm them, then Undo, checking the files themselves each time. The
// folder is the suite's own two files, so nothing else can be moved.
//   node suites/organise.mjs
import { copyFileSync, existsSync, mkdirSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { close, go, launch, lib, resize, sleep, waitIndexed } from '../lib/app.mjs'
import { fixture, libraryCopy } from '../lib/fixtures.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('organise')
const { must } = s
const same = (path) => path.toLowerCase().replaceAll('\\', '/')

// Two photos of the library fixture, copied flat into a folder of their own.
const files = join(s.run, 'files')
mkdirSync(files)
const corpus = fixture('library')
const wanted = ['IMG_00400.jpg', 'IMG_00401.jpg']
for (const entry of readdirSync(corpus, { recursive: true })) {
  const name = wanted.find((w) => entry.endsWith(w))
  if (name) copyFileSync(join(corpus, entry), join(files, name))
}

const report = {}
const app = await launch({ exe: s.exe, dataDir: await libraryCopy(join(s.run, 'data')), port: s.port })
const { page } = app
s.watch(page)
async function clickText(selector, text) {
  const handle = await page.evaluateHandle(
    (selector, text) => [...document.querySelectorAll(selector)].find((e) => e.textContent.trim() === text),
    selector,
    text,
  )
  const element = handle.asElement()
  if (!element) throw new Error(`missing ${text}`)
  await element.click()
  await handle.dispose()
}
try {
  await resize(page, 1440, 900)
  const sources = await lib(page, 'AddSources', [files])
  const source = sources.find((x) => same(x.path) === same(files))
  if (!source) throw new Error('the isolated source was not added')
  await waitIndexed(page, { timeout: 120000 })
  await page.reload()
  await page.waitForSelector('.scroller')
  let preflight
  for (let attempt = 0; attempt < 50; attempt++) {
    preflight = await lib(page, 'PlanOrganise', '{Year}/{Name}', source.id)
    if (preflight.moves.length === 2) break
    await sleep(200)
  }
  report.preflight = preflight
  must(
    'the engine plans exactly the two isolated files',
    preflight.moves.length === 2,
    `${preflight.moves.length} moves`,
  )

  await go(page, '/settings/organise', 0)
  await page.waitForSelector('#organise-source')
  await page.click('#organise-source')
  await clickText('[role="option"]', source.path)
  await sleep(350)
  await page.focus('#organise-scheme')
  await page.keyboard.down('Control')
  await page.keyboard.press('a')
  await page.keyboard.up('Control')
  await page.keyboard.type('{Year}/{Name}')
  await clickText('button', 'Preview the moves')
  await page.waitForFunction(() => document.querySelectorAll('.move').length === 2, { timeout: 30000 })
  const moves = await page.evaluate(() =>
    [...document.querySelectorAll('.move')].map((row) => [...row.querySelectorAll('.path')].map((p) => p.title)),
  )
  const own = (path) => same(path).startsWith(same(files) + '/')
  must(
    'the preview lists exactly the two isolated copies',
    moves.length === 2 && moves.every(([from, to]) => own(from) && own(to)),
    moves,
  )
  report.moves = moves
  await page.screenshot({ path: join(s.out, 'preview.png') })

  await clickText('button', 'Move 2 files…')
  await clickText('[role="dialog"] button', 'Move files')
  await page.waitForFunction(
    () => document.querySelector('.finished')?.textContent.includes('Finished. Your files are in their new folders.'),
    { timeout: 30000, polling: 16 },
  )
  must(
    'the confirmed action moves each file on disk',
    moves.every(([from, to]) => !existsSync(from) && existsSync(to)),
  )

  await page.waitForFunction(() => !document.querySelector('[role="dialog"]'), { timeout: 5000 })
  await clickText('.run-entry button', 'Undo')
  await page.waitForFunction(
    () => document.querySelector('[data-confirm="ok"]')?.textContent.trim() === 'Put files back',
    { timeout: 5000 },
  )
  await clickText('[role="dialog"] button', 'Put files back')
  await page.waitForFunction(
    () => document.querySelector('.finished')?.textContent.includes('Every file is back where it was.'),
    { timeout: 30000, polling: 16 },
  )
  must(
    'Undo puts each file back where it was',
    moves.every(([from, to]) => existsSync(from) && !existsSync(to)),
  )
  must('the screen reports no failure', !(await page.$('[data-organise-error]')))
  const runs = await lib(page, 'OrganiseRuns')
  must('the engine and the screen agree the run is undone', runs[0]?.state === 'undone', runs[0]?.state)
  report.runs = runs
  await page.screenshot({ path: join(s.out, 'undone.png') })
  s.check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors().join(' || '))
} catch (e) {
  s.check('organise ran to the end', false, String(e))
  report.screen = await page.evaluate(() => document.body.innerText.slice(-1500)).catch(() => 'page unavailable')
  await page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  await close(app)
  s.finish(report)
}
