// The files and libraries the suites run on. Everything is generated (or, for
// the camera RAW samples, downloaded once) on first use and kept under .work
// and .cache, so the suites need nothing outside the repository.
import { execFileSync } from 'node:child_process'
import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { close, launch, lib, search, unthrottle, waitIndexed } from './app.mjs'
import { BASE_PORT, CACHE, E2E, EXE, FFMPEG, ROOT, WORK } from './suite.mjs'

const PYTHON = process.env.PYTHON ?? 'python'
/** Photos in the generated library. Perf runs want more: LUMEN_E2E_PHOTOS=3000. */
export const PHOTOS = Number(process.env.LUMEN_E2E_PHOTOS ?? 600)

const python = (script, ...args) =>
  execFileSync(PYTHON, [join(E2E, 'fixtures', script), ...args], { cwd: ROOT, stdio: 'inherit' })

/**
 * Returns the folder of a generated fixture, building it the first time:
 *   library  dated synthetic JPEGs in year/month folders
 *   videos   one video WebView2 plays directly, two that need conversion
 *   media    photos (one turned, one Display P3), a large TIFF, a HEIC, two
 *            videos with two audio tracks and subtitles
 *   formats  camera RAW of every family, JPEG XL, APNG, a three-page TIFF,
 *            MPEG/TS/Ogg/ASF videos, and files that must be skipped
 */
export function fixture(name) {
  const dir = join(WORK, 'fixtures', name === 'library' ? `library-${PHOTOS}` : name)
  const ready = join(dir, '.ready')
  if (existsSync(ready)) return dir
  if (!existsSync(FFMPEG)) throw new Error(`${FFMPEG} is missing: run scripts/deps.sh`)
  rmSync(dir, { recursive: true, force: true })
  mkdirSync(dir, { recursive: true })
  console.log(`building the ${name} fixture in ${dir}`)
  if (name === 'formats') python('fetch_raw.py', '--acceptance', join(CACHE, 'raw'))
  python(
    'build.py',
    name,
    dir,
    ...(name === 'library' ? [String(PHOTOS)] : name === 'formats' ? [join(CACHE, 'raw')] : []),
  )
  writeFileSync(ready, '')
  return dir
}

/** A copy of a fixture a suite may change (rename, move) without spoiling it for the next. */
export function fixtureCopy(name, to) {
  cpSync(fixture(name), to, { recursive: true, filter: (p) => !p.endsWith('.ready') })
  return to
}

/**
 * The base library: the library fixture indexed to the end (thumbnails,
 * embeddings, faces) by the build under test, then given some user state so
 * every screen has something to show. Built once; suites work on copies.
 */
export async function baseLibrary() {
  const dir = join(WORK, 'libraries', `base-${PHOTOS}`)
  const ready = join(dir, 'e2e-ready.json')
  if (existsSync(ready)) return dir
  const corpus = fixture('library')
  rmSync(dir, { recursive: true, force: true })
  console.log(`indexing ${PHOTOS} photos into the base library (once)`)
  const app = await launch({ exe: EXE, dataDir: dir, port: BASE_PORT })
  try {
    const { page } = app
    await unthrottle(page)
    await lib(page, 'AddSources', [corpus])
    // Off the Welcome screen at once, so nothing on it can be clicked.
    await page.reload()
    const progress = await waitIndexed(page, { ai: true, atLeast: PHOTOS, timeout: 3_600_000, log: console.log })

    const rule = { kind: '', favourites: false, from: '', to: '', tag: 0, person: 0 }
    for (let id = 1; id <= 12; id++) await lib(page, 'SetFavourite', id, true)
    const range = (from, n) => Array.from({ length: n }, (_, i) => from + i)
    await lib(page, 'CreateAlbum', 'Holiday in the mountains', 'manual', rule, range(20, 30))
    await lib(page, 'CreateAlbum', 'All my favourites', 'smart', { ...rule, favourites: true }, [])
    await lib(
      page,
      'CreateAlbum',
      'An album with a very long name that has to be shortened somewhere sensible',
      'manual',
      rule,
      [60, 61],
    )
    await lib(page, 'TrashAssets', [100, 101, 102])
    await search(page, 'CreateLabel', 'acceptance label')
    const state = {
      photos: progress.total,
      albums: (await lib(page, 'Albums')).map((a) => `${a.name}:${a.count}`),
      trash: await lib(page, 'TrashCount'),
      events: (await lib(page, 'Events')).length,
      things: (await search(page, 'Things')).length,
    }
    await close(app)
    writeFileSync(ready, JSON.stringify(state, null, 2))
    console.log(`base library ready: ${JSON.stringify(state)}`)
  } catch (e) {
    await close(app)
    throw e
  }
  return dir
}

/** A fresh copy of the base library for one suite: whatever it does stays its own. */
export async function libraryCopy(to) {
  const base = await baseLibrary()
  rmSync(to, { recursive: true, force: true })
  mkdirSync(to, { recursive: true })
  // The WebView's profile, the log and crash reports belong to the run that made them.
  for (const entry of readdirSync(base)) {
    if (['webview', 'crashes', 'lumen.log', 'e2e-ready.json'].includes(entry)) continue
    cpSync(join(base, entry), join(to, entry), { recursive: true })
  }
  return to
}

export const baseState = async () => JSON.parse(readFileSync(join(await baseLibrary(), 'e2e-ready.json'), 'utf8'))
