// A previous executable writes user state and durable work; the candidate
// opens that same scratch library, preserves the state and finishes the work.
// LUMEN_PREVIOUS_EXE must name a saved build from before a dependency change.
import { execFileSync } from 'node:child_process'
import { existsSync, writeFileSync } from 'node:fs'
import { delimiter, join, resolve } from 'node:path'
import { isDeepStrictEqual } from 'node:util'
import { close, launch, lib, search, sleep, unthrottle, waitIndexed } from '../lib/app.mjs'
import { E2E, suite } from '../lib/suite.mjs'

const previous = process.env.LUMEN_PREVIOUS_EXE && resolve(process.env.LUMEN_PREVIOUS_EXE)
if (!previous || !existsSync(previous)) throw new Error('LUMEN_PREVIOUS_EXE must name the previous production build')
const previousRuntime = process.env.LUMEN_PREVIOUS_RUNTIME_DIR && resolve(process.env.LUMEN_PREVIOUS_RUNTIME_DIR)
const s = suite('upgrade')
const dataDir = join(s.run, 'data')
const corpus = join(s.run, 'photos')
const count = 24
const pin = '583129'
let app

function ocrJobs() {
  return JSON.parse(
    execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'lib', 'upgrade_jobs.py'), dataDir], {
      encoding: 'utf8',
    }),
  )
}

async function snapshot(page) {
  const asset = await lib(page, 'Asset', 1)
  return {
    sources: await lib(page, 'Sources'),
    albums: await lib(page, 'Albums'),
    membership: await lib(page, 'AssetAlbums', 1),
    asset: {
      favourite: asset.favourite,
      takenAt: asset.takenAt,
      dateFrom: asset.dateFrom,
      originalDate: asset.originalDate,
      originalLocation: asset.originalLocation,
      lat: asset.lat,
      lon: asset.lon,
      paths: asset.paths,
    },
    tags: await search(page, 'AssetTags', 1),
    blocks: await lib(page, 'Blocks'),
    preferences: await lib(page, 'Preferences'),
    settings: await lib(page, 'Settings'),
  }
}

try {
  execFileSync(process.env.PYTHON ?? 'python', [join(E2E, 'fixtures', 'build.py'), 'library', corpus, String(count)], {
    stdio: 'inherit',
  })
  app = await launch({
    exe: previous,
    dataDir,
    port: s.port,
    env: previousRuntime
      ? {
          LUMEN_RUNTIME_DIR: previousRuntime,
          PATH: `${previousRuntime}${delimiter}${process.env.PATH ?? process.env.Path ?? ''}`,
        }
      : {},
  })
  s.watch(app.page)
  await unthrottle(app.page)
  await lib(app.page, 'AddSources', [corpus])
  await app.page.reload()
  await waitIndexed(app.page, { ai: true, atLeast: count, log: console.log })
  await lib(app.page, 'SetFavourite', 1, true)
  const rule = { kind: '', favourites: false, from: '', to: '', tag: 0, person: 0 }
  await lib(app.page, 'CreateAlbum', 'Café 2 — manual', 'manual', rule, [1, 2, 3])
  await lib(app.page, 'CreateAlbum', 'Favourites 10', 'smart', { ...rule, favourites: true }, [])
  await search(app.page, 'TagAsset', 1, 'Manual café 2')
  await search(app.page, 'TagAsset', 1, 'Screenshots')
  await search(app.page, 'TagAsset', 2, 'Screenshots')
  await lib(app.page, 'SetTakenAt', [1], 1704067200)
  await lib(app.page, 'SetLocation', [1], 31.52, 74.36)
  const preferences = await lib(app.page, 'Preferences')
  await lib(app.page, 'SetPreferences', { ...preferences, theme: 'dark', startView: 'albums', motion: 'reduced' })
  await lib(app.page, 'CreateBlockPIN', pin)
  await lib(app.page, 'UnlockBlocks', pin)
  await lib(app.page, 'SetBlock', 'asset', 2, '', 'photos')
  await lib(app.page, 'SetPaused', true)
  await lib(app.page, 'SetOCR', true)
  const pending = await lib(app.page, 'Progress')
  s.must('the previous build leaves durable OCR work queued', pending.ocr > 0, pending)
  const before = await snapshot(app.page)
  s.must(
    'the previous build writes the intended user state',
    before.sources.length === 1 &&
      before.albums.length === 2 &&
      before.membership.length === 1 &&
      before.asset.favourite === true &&
      before.asset.takenAt === 1704067200 &&
      before.asset.lat === 31.52 &&
      before.asset.lon === 74.36 &&
      before.tags.some((tag) => tag.name === 'Manual café 2') &&
      before.blocks.length === 1 &&
      before.preferences.theme === 'dark' &&
      before.settings.ocr === true,
    before,
  )
  writeFileSync(join(s.out, 'before.json'), JSON.stringify({ state: before, progress: pending }, null, 2))
  await close(app)
  app = undefined
  const queued = ocrJobs()
  const unfinished = queued.filter((job) => job.status === 'pending' || job.status === 'waiting')
  s.must(
    'OCR jobs for visible and blocked photos remain on disk after the previous build closes',
    [1, 2].every((id) => unfinished.some((job) => job.asset_id === id)),
    queued,
  )
  writeFileSync(join(s.out, 'queued-jobs.json'), JSON.stringify(queued, null, 2))

  app = await launch({ exe: s.exe, dataDir, port: s.port })
  s.watch(app.page)
  // The visible photo is the control: a refusal alone could mean a bad URL.
  const [visibleStatus, blockedStatus] = await app.page.evaluate(async () =>
    Promise.all([1, 2].map(async (id) => (await fetch(`/media/thumb/${id}/256?v=1`)).status)),
  )
  s.must(
    'blocked media remains inaccessible after the upgrade while visible media is served',
    visibleStatus === 200 && blockedStatus >= 400,
    { visibleStatus, blockedStatus },
  )
  s.must('the block PIN survives the upgrade', await lib(app.page, 'BlockPINConfigured'))
  await lib(app.page, 'UnlockBlocks', pin)
  const after = await snapshot(app.page)
  writeFileSync(join(s.out, 'after.json'), JSON.stringify(after, null, 2))
  for (const key of Object.keys(before))
    s.check(`${key} survive opening the library with the candidate`, isDeepStrictEqual(before[key], after[key]), {
      before: before[key],
      after: after[key],
    })
  await unthrottle(app.page)
  let progress
  let completedJobs
  const deadline = Date.now() + 300_000
  do {
    progress = await lib(app.page, 'Progress')
    completedJobs = ocrJobs()
    if (completedJobs.every((job) => job.status === 'done') && progress.ocr === 0 && !progress.working.ocr) break
    await sleep(500)
  } while (Date.now() < deadline)
  s.check(
    'the candidate completes the queued OCR work without losing assets',
    progress.ocr === 0 && progress.total === pending.total && progress.errors === 0,
    progress,
  )
  writeFileSync(join(s.out, 'completed.json'), JSON.stringify(progress, null, 2))
  s.check(
    'every queued job is saved as done, including blocked photos',
    unfinished.every((queued) =>
      completedJobs.some(
        (job) => job.asset_id === queued.asset_id && job.status === 'done' && job.model_version && !job.error,
      ),
    ),
    completedJobs,
  )
  writeFileSync(join(s.out, 'completed-jobs.json'), JSON.stringify(completedJobs, null, 2))
  s.check('no uncaught frontend errors in either build', s.appErrors().length === 0, s.appErrors())
} catch (error) {
  s.check('the upgrade workflow completed', false, String(error))
} finally {
  if (app) await close(app)
  s.finish()
}
