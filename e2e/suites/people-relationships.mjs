// Me and relationships through the real People controls, search, merge
// conflicts, a hidden person and a restart, on a scratch library whose faces
// are seeded once (the face model's grouping is not what is under test).
import { execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { close, go, launch, lib, people, resize, search, unthrottle, waitIndexed } from '../lib/app.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const s = suite('people-relationships')
const dataDir = join(s.run, 'data')
const photos = join(s.run, 'photos')
const PHOTOS = 6
const PIN = '724691'
let app
let phase = 'setup'
const requests = []
// Each question flow starts with the engine's refusal, which the UI turns into
// a question: those, and the direct call on a hidden person, are expected.
const refusals = {
  'merge conflict': [['NamePerson', 'person_exists'], ['MergePeople', 'relation_conflict']],
  'replacing Me': [['SetMe', 'relation_conflict']],
  'a hidden person': [['SetRelation', 'not_found']],
}

function watch(page) {
  s.watch(page, {
    context: () => phase,
    expected: (row) => row.kind === 'response' && !!refusals[row.context]?.some(([method, code]) => row.method === method && row.code === code),
  })
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && !['wails.localhost', 'localhost', '127.0.0.1'].includes(url.hostname))
      requests.push(request.url())
  })
}
async function start() {
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  watch(app.page)
  await resize(app.page, 1280, 860)
}
/** Waits until the page satisfies fn(arg), failing with why after a while. */
async function until(fn, arg, why) {
  try {
    await app.page.waitForFunction(fn, { timeout: 15_000 }, arg)
  } catch (error) {
    throw new Error(`${why}: ${error.message}`)
  }
}
const text = (selector) => app.page.$eval(selector, (el) => el.textContent.trim())
const focused = () => app.page.evaluate(() => document.activeElement?.closest('[data-person-role]')?.dataset.personRole ?? document.activeElement?.getAttribute('role') ?? '')
async function personPage(id) {
  await go(app.page, `/people/${id}`)
  await app.page.waitForSelector('[data-person-role]', { visible: true })
}
/** Opens the search for query and waits until it has finished: the first one loads the text model. */
async function searchFor(query) {
  await go(app.page, `/search?q=${encodeURIComponent(query)}`)
  await app.page.waitForSelector('[data-search-summary]', { visible: true })
  await app.page.waitForFunction(() => !document.querySelector('[data-search-searching]'), { timeout: 120_000 })
}
const byName = async () => Object.fromEntries((await people(app.page, 'People')).map((p) => [p.name, p]))

try {
  mkdirSync(photos, { recursive: true })
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', photos, String(PHOTOS)], { stdio: 'inherit' })
  await start()
  await unthrottle(app.page)
  await lib(app.page, 'AddSources', [photos])
  await app.page.reload()
  await waitIndexed(app.page, { ai: true, atLeast: PHOTOS, timeout: 600_000, log: console.log })
  await lib(app.page, 'SetPaused', true)
  const ids = (await lib(app.page, 'Selection', { filter: '' }, [], false)).ids
  s.must('the scratch library holds its photos', ids.length === PHOTOS, ids)
  const faces = JSON.parse(execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'album_preview.py'), 'faces', join(dataDir, 'library.db'), JSON.stringify(ids)], { encoding: 'utf8' }))
  const named = {}
  for (const [i, name] of ['Ali', 'Jane', 'Sara', 'Omar', 'Zoe', 'Sam'].entries())
    named[name] = await people(app.page, 'NameFaces', [faces[i]], name)
  await app.page.reload()

  // Me: offered on People until chosen, chosen with one click.
  await go(app.page, '/people')
  await app.page.waitForSelector('[data-me-prompt]', { visible: true })
  s.check('People asks which person is you', (await text('[data-me-prompt]')).includes('Which one is you?'))
  await app.page.click(`[data-me-choice="${named.Ali}"]`)
  await until(() => !document.querySelector('[data-me-prompt]'), null, 'the Me prompt stayed')
  s.must('choosing yourself marks that person as Me', (await byName()).Ali.me === true)
  await app.page.screenshot({ path: join(s.out, 'people-with-me.png') })

  // A relationship by keyboard alone: open, filter, arrow, Enter; focus returns.
  await personPage(named.Jane)
  await app.page.focus('[data-person-role="relation"]')
  await app.page.keyboard.press('Enter')
  await app.page.waitForSelector('[data-relation-editor] input', { visible: true })
  await until(() => document.activeElement?.closest('[data-relation-editor]') !== null, null, 'the editor did not take focus')
  await app.page.keyboard.type('mu')
  await app.page.keyboard.press('ArrowDown')
  s.check(
    'the highlighted option is announced to screen readers',
    await app.page.$eval('[data-relation-editor] input', (input) => {
      const active = input.getAttribute('aria-activedescendant')
      return !!active && document.getElementById(active)?.getAttribute('aria-selected') === 'true'
    }),
  )
  await app.page.keyboard.press('Enter')
  await until(() => document.querySelector('[data-person-role="relation"]')?.textContent.trim() === 'Mum', null, 'Mum was not shown')
  s.check('focus returns to the relationship control after saving', (await focused()) === 'relation', await focused())
  s.must('Jane is Mum and keeps her name', (await byName()).Jane.relation === 'mother')

  // The user's own word, and a shared built-in relationship.
  await personPage(named.Sara)
  await app.page.click('[data-person-role="relation"]')
  await app.page.waitForSelector('[data-relation-editor] input', { visible: true })
  await app.page.keyboard.type('Coach')
  await app.page.keyboard.press('Enter')
  await until(() => document.querySelector('[data-person-role="relation"]')?.textContent.trim() === 'Coach', null, 'Coach was not shown')
  for (const name of ['Omar', 'Zoe']) await people(app.page, 'SetRelation', named[name], 'friend', '')
  const kept = await byName()
  s.must('a custom label is kept as typed', kept.Sara.relation === 'custom' && kept.Sara.relationLabel === 'Coach', kept.Sara)

  // The editor in a narrow window, the dark theme, forced colours and reduced motion.
  const prefs = await lib(app.page, 'Preferences')
  await lib(app.page, 'SetPreferences', { ...prefs, theme: 'dark', motion: 'reduced' })
  await app.page.reload()
  await resize(app.page, 800, 700)
  const session = await app.page.createCDPSession()
  await session.send('Emulation.setEmulatedMedia', {
    features: [
      { name: 'forced-colors', value: 'active' },
      { name: 'prefers-reduced-motion', value: 'reduce' },
    ],
  })
  await personPage(named.Jane)
  await app.page.click('[data-person-role="relation"]')
  await app.page.waitForSelector('[data-relation-editor] input', { visible: true })
  s.check(
    'the editor fits a narrow window without sideways scrolling',
    await app.page.evaluate(() => {
      const editor = document.querySelector('[data-relation-editor]').getBoundingClientRect()
      return editor.right <= innerWidth && document.scrollingElement.scrollWidth <= innerWidth
    }),
  )
  await app.page.screenshot({ path: join(s.out, 'editor-narrow-dark-forced-colours.png') })
  await app.page.keyboard.press('Escape')
  await until(() => !document.querySelector('[data-relation-editor]'), null, 'Escape did not close the editor')
  s.check('Escape returns focus to the relationship control', (await focused()) === 'relation', await focused())
  await session.send('Emulation.setEmulatedMedia', { features: [] })
  await session.detach()
  await lib(app.page, 'SetPreferences', prefs)
  await app.page.reload()
  await resize(app.page, 1280, 860)

  // Search: me, a relationship, a plural, an ambiguous singular and an unassigned phrase.
  await searchFor('my mum')
  s.check('"my mum" names whom it found', (await text('[data-search-part="person"] .kind')) === 'Jane')
  s.check('"my mum" finds her photo', (await search(app.page, 'Search', 'my mum')).items?.length === 1)
  s.check('"me" finds my photo', (await search(app.page, 'Search', 'me')).items?.length === 1)
  s.check('"my friends" finds either friend', (await search(app.page, 'Search', 'my friends')).items?.length === 2)
  await searchFor('my friend')
  s.must('"my friend" with two friends asks which', !!(await app.page.$('[data-search-ambiguous]')))
  await app.page.screenshot({ path: join(s.out, 'ambiguous-search.png') })
  await app.page.click(`[data-search-only="${named.Omar}"]`)
  await until(() => new URLSearchParams(location.search).get('q') === 'Omar', null, 'choosing one did not narrow the search')
  await searchFor('my dad at the beach')
  s.check('a relationship no one has says so', (await text('[data-search-unassigned]')).includes('No one is set as “my dad” yet'))

  // Merge conflict: naming Omar "Sara" asks Same person, then which relationship stays.
  phase = 'merge conflict'
  await personPage(named.Omar)
  await app.page.click('[data-action="rename"]')
  await app.page.waitForSelector('[data-person-editor] input', { visible: true })
  await app.page.$eval('[data-person-editor] input', (input) => input.select())
  await app.page.keyboard.type('Sara')
  await app.page.keyboard.press('Enter')
  await app.page.locator('[data-person-editor] button ::-p-text(Same person)').click()
  await app.page.waitForSelector('[data-confirm="other"]', { visible: true })
  s.check(
    'the merge explains both relationships',
    (await app.page.$eval('[role="dialog"]', (d) => d.textContent)).includes('One is your Friend and the other your Coach'),
  )
  await app.page.click('[data-confirm="other"]')
  await until((id) => location.pathname === `/people/${id}`, named.Sara, 'the merged person was not shown')
  const merged = await byName()
  s.must('the merge keeps the relationship chosen', merged.Sara.relation === 'friend' && !merged.Omar, merged.Sara)

  // Replacing Me is explained and can be declined.
  phase = 'replacing Me'
  await personPage(named.Jane)
  await app.page.click('[aria-label="Person actions"]')
  await app.page.locator('[role="menuitem"] ::-p-text(This is me)').click()
  await app.page.waitForSelector('[data-confirm="cancel"]', { visible: true })
  const question = await app.page.$eval('[role="dialog"]', (d) => d.textContent)
  s.check('replacing Me names who is Me and what is lost', question.includes('Ali is Me now') && question.includes('“Mum” will be removed'), question)
  await app.page.click('[data-confirm="cancel"]')
  await until(() => !document.querySelector('[role="dialog"]'), null, 'the question stayed open')
  const declined = await byName()
  s.must('declining keeps Me and the relationship', declined.Ali.me && declined.Jane.relation === 'mother')

  // Restart: every choice is still there.
  phase = 'restart'
  await close(app)
  app = undefined
  await start()
  const restarted = await byName()
  s.must(
    'Me and relationships survive a restart',
    restarted.Ali.me && restarted.Jane.relation === 'mother' && restarted.Sara.relation === 'friend' && restarted.Zoe.relation === 'friend',
    restarted,
  )
  await personPage(named.Jane)
  s.check('the person page shows the kept relationship after the restart', (await text('[data-person-role="relation"]')) === 'Mum')

  // A hidden Mum is neither named nor found by "my mum".
  phase = 'a hidden person'
  await lib(app.page, 'CreateBlockPIN', PIN)
  await lib(app.page, 'SetBlock', 'person', named.Jane, '', 'label')
  await searchFor('my mum')
  s.check('a hidden Mum blocks the search', !!(await app.page.$('[data-search-blocked]')))
  s.check('the hidden Mum is not named', !(await app.page.$eval('[data-search-summary]', (el) => el.textContent)).includes('Jane'))
  const direct = await people(app.page, 'SetRelation', named.Jane, 'friend', '').then(() => 'changed', (error) => String(error))
  s.check('a direct call cannot relabel a hidden person', direct.includes('not_found'), direct)

  s.check('no external network requests', requests.length === 0, requests)
  await s.settleErrors()
  s.check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors())
} catch (error) {
  s.check('the relationship workflow ran to the end', false, String(error?.stack ?? error))
  if (app) await app.page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  if (app) await close(app)
  s.finish()
}
