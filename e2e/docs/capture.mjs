// Captures the People face-review screenshot from the real production WebView,
// on a fresh library of the pinned public sample photos (fixtures.py).
// Opt-in documentation tooling, outside run.mjs.
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { basename, join } from 'node:path'
import { close, go, launch, lib, people, sleep, unthrottle, waitIndexed } from '../lib/app.mjs'
import { EXE, ROOT, version } from '../lib/suite.mjs'

// The framing of the other screenshots: 1440 × 900 CSS pixels at 125 % scaling,
// emulated so the result does not depend on this PC's display.
const VIEWPORT = { width: 1440, height: 900, deviceScaleFactor: 1.25 }
const run = join(ROOT, 'e2e', '.work', 'docs-capture', new Date().toISOString().replace(/[:.]/g, '-'))
const source = join(run, 'source')
const out = join(ROOT, 'docs', 'screenshots')
const media = join(ROOT, 'e2e', 'docs', 'media.json')
mkdirSync(run, { recursive: true })
execFileSync(process.env.PYTHON ?? 'python', [join(ROOT, 'e2e', 'docs', 'fixtures.py'), source], { stdio: 'inherit' })
const expected = JSON.parse(readFileSync(media, 'utf8')).photos.length + 5 + 1
const hash = (path) => createHash('sha256').update(readFileSync(path)).digest('hex')
const manifest = {
  capturedAt: new Date().toISOString(),
  version: version(),
  sourceCommit: execFileSync('git', ['rev-parse', 'HEAD'], { cwd: ROOT }).toString().trim(),
  executableSHA256: hash(EXE),
  mediaManifestSHA256: hash(media),
  screenshots: [],
  // The gallery's other images are earlier captures, kept as they are; their
  // record carries over unchanged.
  kept: JSON.parse(readFileSync(join(out, 'capture.json'), 'utf8')).kept,
  checks: [],
}
const errors = []
const external = []
const app = await launch({ exe: EXE, dataDir: join(run, 'data'), port: 9446 })
const { page } = app
page.on('pageerror', (error) => errors.push(String(error)))
page.on('request', (request) => {
  const url = new URL(request.url())
  if (
    ['http:', 'https:'].includes(url.protocol) &&
    !['wails.localhost', '127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)
  )
    external.push(url.href)
})
const check = (name, actual) => {
  assert.ok(actual, name)
  manifest.checks.push(name)
  console.log(`PASS ${name}`)
}
async function shot(name, caption) {
  // Only the images on screen: lazy ones further down never load until scrolled to.
  await page.waitForFunction(() =>
    [...document.images]
      .filter((image) => {
        const box = image.getBoundingClientRect()
        return box.width > 0 && box.bottom > 0 && box.top < innerHeight
      })
      .every((image) => image.complete),
  )
  await page.evaluate(() => document.fonts.ready)
  // Let transitions and hover controls finish before publishing the same framing.
  await sleep(900)
  const dimensions = await page.evaluate(() => ({
    width: Math.round(innerWidth * devicePixelRatio),
    height: Math.round(innerHeight * devicePixelRatio),
    cssWidth: innerWidth,
    cssHeight: innerHeight,
    devicePixelRatio,
  }))
  check(
    `${name} has the gallery's framing`,
    dimensions.cssWidth === VIEWPORT.width && dimensions.cssHeight === VIEWPORT.height,
  )
  const path = join(out, `${name}.webp`)
  await page.screenshot({ path, type: 'webp', quality: 82 })
  manifest.screenshots.push({
    file: `${name}.webp`,
    caption,
    ...dimensions,
    bytes: readFileSync(path).length,
    sha256: hash(path),
  })
  console.log(`CAPTURE ${name}`)
}
try {
  await unthrottle(page)
  await lib(page, 'SetPauseFullScreen', false)
  await lib(page, 'SetVideos', true)
  await lib(page, 'AddSources', [join(source, 'library')])
  await page.reload()
  await waitIndexed(page, { ai: true, atLeast: expected, timeout: 3_600_000, log: console.log })
  check(`all ${expected} sample files indexed`, (await lib(page, 'Progress')).total === expected)

  // The faces stage groups the portrait frames by itself; naming the group is
  // the user's part. Assigning the faces by hand is only the fallback.
  const days = await lib(page, 'Days', '2026-07-04', '2026-08-09', { filter: '' })
  const assets = await Promise.all(days.flatMap((day) => day.items ?? []).map((item) => lib(page, 'Asset', item.id)))
  const portraits = assets.filter((asset) => asset.paths.some((path) => basename(path).includes('_portrait')))
  check('all five portrait frames are in the library', portraits.length === 5)
  const faces = (await Promise.all(portraits.map((asset) => people(page, 'AssetFaces', asset.id)))).flat()
  check('the face model found the portrait faces', faces.length >= 3)
  const grouped = faces.find((face) => face.person !== 0)?.person ?? 0
  let person = grouped
  if (grouped) await people(page, 'NamePerson', grouped, 'Sample astronaut')
  else
    person = await people(
      page,
      'NameFaces',
      faces.map((face) => face.id),
      'Sample astronaut',
    )
  manifest.personGroupedByClustering = grouped !== 0
  check('the named person persists', (await people(page, 'Person', person)).name === 'Sample astronaut')

  // Favourites as the other screenshots' library had them, for the same sidebar.
  const all = (await lib(page, 'Selection', { filter: '' }, [], false)).ids
  for (let i = 3; i < all.length; i += 23) await lib(page, 'SetFavourite', all[i], true)
  const prefs = await lib(page, 'Preferences')
  await lib(page, 'SetPreferences', { ...prefs, theme: 'light', motion: 'reduced' })
  await page.setViewport(VIEWPORT)
  await page.reload()
  await go(page, `/people/${person}/faces`)
  await page.waitForSelector('[data-face]')
  await page.click('[data-face] .face-tile')
  await page.mouse.move(2, 2)
  await shot('faces', 'Reviewing the faces of a named person, one selected.')
  check('no uncaught page errors', errors.length === 0)
  check('no application request left the PC', external.length === 0)
  manifest.externalRequests = external
  writeFileSync(join(out, 'capture.json'), `${JSON.stringify(manifest, null, 2)}\n`)
} catch (error) {
  await page.screenshot({ path: join(run, 'failure.png') }).catch(() => {})
  console.error(await page.evaluate(() => document.body.innerText.slice(-2200)).catch(() => 'page unavailable'))
  throw error
} finally {
  await close(app)
  writeFileSync(join(run, 'report.json'), `${JSON.stringify({ ...manifest, errors, external }, null, 2)}\n`)
}
