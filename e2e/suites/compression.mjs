// Every file and library belongs to this suite's scratch folder.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { basename, join } from 'node:path'
import {
  close,
  compression,
  go,
  launch,
  lib,
  media,
  resize,
  sleep,
  unthrottle,
  waitIndexed,
} from '../lib/app.mjs'
import { ROOT, suite } from '../lib/suite.mjs'

const s = suite('compression')
const photos = join(s.run, 'photos')
execFileSync('python', [
  join(ROOT, 'e2e', 'fixtures', 'compression.py'),
  photos,
])
const hash = (path) =>
  createHash('sha256').update(readFileSync(path)).digest('hex')
const originals = new Map(
  [
    'screenshot.png',
    'photo.jpg',
    'duplicate.jpg',
    'scan.png',
    'profile.png',
  ].map((name) => [name, hash(join(photos, name))]),
)
const dataDir = join(s.run, 'data')
let app
const external = []
const start = async () => {
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  s.watch(app.page)
  app.page.on('request', (request) => {
    const url = new URL(request.url())
    if (
      !['wails.localhost', '127.0.0.1', 'localhost', ''].includes(
        url.hostname,
      ) &&
      !['data:', 'blob:'].includes(url.protocol)
    )
      external.push(request.url())
  })
  await unthrottle(app.page)
  await lib(app.page, 'SetPauseFullScreen', false)
  return app.page
}
const options = (mode = 'compatible', effort = 'fast', minSavings = 0) => ({
  mode,
  effort,
  minSavings,
  threads: 1,
})
async function outcomes(page, id) {
  const result = await compression(page, 'Jobs', id, 0)
  return result.jobs ?? []
}
async function settled(
  page,
  id,
  states = ['ready', 'done', 'failed', 'cancelled', 'paused'],
) {
  const until = Date.now() + 300_000
  while (Date.now() < until) {
    const runs = await compression(page, 'Runs')
    const run = runs.find((run) => run.id === id)
    if (run && states.includes(run.state)) return outcomes(page, id)
    await sleep(200)
  }
  throw new Error(`compression run ${id} did not settle`)
}
async function prepare(
  page,
  asset,
  mode = 'compatible',
  directory = '',
  minSavings = 0,
) {
  const run = await compression(
    page,
    'Plan',
    [
      {
        assetId: asset.id,
        path: asset.paths.find((p) => basename(p) !== 'duplicate.jpg') ?? '',
      },
    ],
    directory,
    options(mode, 'fast', minSavings),
  )
  await compression(page, 'Analyse', run.id)
  return { run, job: (await settled(page, run.id))[0] }
}
try {
  let page = await start()
  await lib(page, 'AddSources', [photos])
  await waitIndexed(page, { atLeast: 6, timeout: 300_000, log: console.log })
  // Direct service setup does not refresh the Welcome screen's source list.
  await page.reload()
  await page.waitForSelector('.scroller')
  const days = await lib(page, 'Days', '0001-01-01', '9999-12-31', {
    filter: 'all',
  })
  const assets = await Promise.all(
    days
      .flatMap((d) => d.items ?? [])
      .map((item) => lib(page, 'Asset', item.id)),
  )
  const named = (name) =>
    assets.find((asset) => asset.paths.some((path) => basename(path) === name))
  s.must(
    'all static fixture families index',
    assets.length === 6,
    assets.map((a) => a.ext),
  )
  s.must(
    'pinned local codecs are available',
    await compression(page, 'Available'),
  )

  await s.guard('entry points', async () => {
    const source = named('screenshot.png')
    await go(page, '/')
    await page.waitForSelector(`#cell-${source.id}`)
    await page.click(`#cell-${source.id}`)
    await page.waitForFunction(() =>
      [...document.querySelectorAll('button')].some(
        (b) => b.textContent.trim() === 'Compress losslessly…' && !b.disabled,
      ),
    )
    await page.evaluate(() =>
      [...document.querySelectorAll('button')]
        .find(
          (b) => b.textContent.trim() === 'Compress losslessly…' && !b.disabled,
        )
        ?.click(),
    )
    await page.waitForSelector('[data-compression-plan]')
    s.check(
      'grid selection opens an unanalysed compression request',
      await page.evaluate(
        (id) =>
          location.pathname === '/compression' &&
          document.body.textContent.includes(`#${id}`),
        source.id,
      ),
    )
    await go(page, '/')
    await page.waitForSelector(`#cell-${source.id}`)
    await page.click(`#cell-${source.id}`, { count: 2 })
    await page.waitForSelector(
      '.viewer button[aria-label="Compress losslessly…"]',
    )
    await page.click('.viewer button[aria-label="Compress losslessly…"]')
    await page.waitForSelector('[data-compression-plan]')
    s.check(
      'viewer opens the same compression workspace',
      await page.evaluate(
        (id) =>
          location.pathname === '/compression' &&
          document.body.textContent.includes(`#${id}`),
        source.id,
      ),
    )
    s.check(
      'entry actions do not create a durable run',
      (await compression(page, 'Runs')).length === 0,
    )
  })

  await s.guard('copies', async () => {
    const source = named('screenshot.png')
    await lib(page, 'SetFavourite', source.id, true)
    const { run, job } = await prepare(page, source)
    s.must(
      'analysis produces a smaller verified candidate without publication',
      job?.state === 'ready' &&
        job.outputSize < job.size &&
        !existsSync(job.to),
      job,
    )
    // The run was planned through the service, which the open page cannot
    // know of: it reloads on its own actions and while a run is working.
    // Open it afresh, as it is found after a restart.
    await go(page, '/')
    await go(page, '/compression')
    await page.waitForFunction(() =>
      [...document.querySelectorAll('button')].some((button) =>
        button.textContent.includes('Run '),
      ),
    )
    await page.evaluate(
      (id) =>
        [...document.querySelectorAll('button')]
          .find((b) => b.textContent.includes(`Run ${id} ·`))
          ?.click(),
      run.id,
    )
    await page.waitForSelector('[data-compression-commit]')
    await page.click('[data-compression-commit]')
    await page.waitForFunction(
      () => !!document.querySelector('[role="alertdialog"], [role="dialog"]'),
    )
    await page.evaluate(() => {
      const dialog = document.querySelector(
        '[role="alertdialog"], [role="dialog"]',
      )
      ;[...dialog.querySelectorAll('button')]
        .find((b) => b.textContent.includes('Save selected copies'))
        ?.click()
    })
    const done = (await settled(page, run.id, ['done', 'failed']))[0]
    s.must(
      'reviewed UI action publishes and registers the related copy',
      done.state === 'done' &&
        done.outputAssetId !== source.id &&
        existsSync(done.to),
      done,
    )
    s.check(
      'new content retains the favourite',
      (await lib(page, 'Asset', done.outputAssetId)).favourite,
    )
    s.check(
      'compatible source stays unchanged',
      hash(source.paths[0]) === originals.get('screenshot.png'),
    )
    await page.screenshot({ path: join(s.out, 'compatible-copy.png') })
  })

  await s.guard('restart', async () => {
    const { run, job } = await prepare(page, named('scan.png'))
    s.must('greyscale candidate is ready', job.state === 'ready', job)
    await close(app)
    app = undefined
    page = await start()
    const after = (await outcomes(page, run.id))[0]
    s.check(
      'ready review survives a graceful restart without publication',
      after.state === 'ready' && !existsSync(after.to),
      after,
    )
    await compression(page, 'Cancel', run.id)
    s.check(
      'cancellation discards the uncommitted candidate',
      (await outcomes(page, run.id))[0].state === 'cancelled' &&
        !existsSync(after.to),
    )
  })

  await s.guard('reversible', async () => {
    const source = named('photo.jpg')
    const { run, job } = await prepare(page, source, 'reversible')
    s.must(
      'JPEG reconstruction is ready and records the duplicate choice',
      job.state === 'ready' &&
        job.copies === 2 &&
        job.verification === 'jpeg-full-sha256',
      job,
    )
    await compression(page, 'Commit', run.id, [job.id])
    const done = (await settled(page, run.id, ['done', 'failed']))[0]
    s.must(
      'JPEG XL is indexed as its own content',
      done.state === 'done' && done.outputAssetId > 0,
      done,
    )
    await waitIndexed(page, { atLeast: 8, timeout: 300_000 })
    const output = await lib(page, 'Asset', done.outputAssetId)
    s.check(
      'JPEG XL keeps capture date and camera after native indexing',
      output.takenAt === source.takenAt && output.camera === source.camera,
      {
        source: [source.takenAt, source.camera],
        output: [output.takenAt, output.camera],
      },
    )
    s.check(
      'JPEG XL inspector reads its embedded lens',
      (await media(page, 'Inspect', done.outputAssetId)).lens.includes(
        'Fixture lens',
      ),
    )
    s.check(
      'JPEG XL keeps the source upright dimensions',
      output.width === source.width && output.height === source.height,
      {
        source: [source.width, source.height],
        output: [output.width, output.height],
      },
    )
    s.check(
      'JPEG XL exposes native preview capability',
      output.caps.preview && output.width > 0 && output.height > 0,
      output.caps,
    )
    const rendered = await media(
      page,
      'RenderPage',
      done.outputAssetId,
      2048,
      0,
    )
    const url = `/media/render/${done.outputAssetId}/2048?v=${encodeURIComponent(rendered.revision)}`
    s.check(
      'JPEG XL renders through the native decoder',
      await page.evaluate(async (url) => {
        const response = await fetch(url)
        const blob = await response.blob()
        const bitmap = await createImageBitmap(blob)
        const okay = response.ok && bitmap.width > 0
        bitmap.close()
        return okay
      }, url),
    )
    s.check(
      'external XMP is mapped without modifying either original',
      readFileSync(done.to + '.xmp').equals(
        readFileSync(join(photos, 'photo.jpg.xmp')),
      ),
    )
    const restore = await compression(page, 'Restore', run.id, done.id, '')
    await compression(page, 'Analyse', restore.id)
    const restored = (await settled(page, restore.id))[0]
    s.must(
      'restoration is separately reviewed',
      restored.state === 'ready' && !existsSync(restored.to),
      restored,
    )
    await compression(page, 'Commit', restore.id, [restored.id])
    const final = (await settled(page, restore.id, ['done', 'failed']))[0]
    s.check(
      'restored JPEG reproduces the complete original SHA256',
      final.state === 'done' && hash(final.to) === originals.get('photo.jpg'),
      final,
    )
  })

  await s.guard('partial', async () => {
    const inputs = [named('profile.png'), named('unsupported.bmp')].map(
      (a) => ({ assetId: a.id, path: '' }),
    )
    const run = await compression(page, 'Plan', inputs, '', options('convert'))
    await compression(page, 'Analyse', run.id)
    const jobs = await settled(page, run.id)
    s.check(
      'unsupported formats and profile mappings skip independently',
      jobs.length === 2 &&
        jobs.every((j) => j.state === 'skipped' && j.reason === 'unsupported'),
      jobs,
    )
    const noGain = await prepare(
      page,
      named('already-optimised.png'),
      'compatible',
      '',
      90,
    )
    s.check(
      'minimum saving skips an insignificant result',
      noGain.job.state === 'skipped' && noGain.job.reason === 'no_gain',
      noGain.job,
    )
    const race = await prepare(page, named('scan.png'))
    writeFileSync(race.job.to, 'keep a later destination')
    await compression(page, 'Commit', race.run.id, [race.job.id])
    await settled(page, race.run.id, ['done', 'failed'])
    s.check(
      'destination collision keeps later work unchanged',
      readFileSync(race.job.to, 'utf8') === 'keep a later destination',
    )
  })

  await s.guard('png conversion', async () => {
    const source = named('screenshot.png')
    const { run, job } = await prepare(page, source, 'convert')
    s.must('PNG conversion verifies exact samples', job.state === 'ready', job)
    await compression(page, 'Commit', run.id, [job.id])
    const done = (await settled(page, run.id, ['done', 'failed']))[0]
    s.must(
      'PNG conversion publishes a related JPEG XL copy',
      done.state === 'done' && done.outputAssetId > 0,
      done,
    )
    await waitIndexed(page, { atLeast: 9, timeout: 300_000 })
    const rendered = await media(
      page,
      'RenderPage',
      done.outputAssetId,
      2048,
      0,
    )
    s.check(
      'converted PNG renders in the native viewer',
      await page.evaluate(
        async ({ id, revision }) => {
          const response = await fetch(
            `/media/render/${id}/2048?v=${encodeURIComponent(revision)}`,
          )
          const bitmap = await createImageBitmap(await response.blob())
          const okay =
            response.ok && bitmap.width === 1600 && bitmap.height === 1000
          bitmap.close()
          return okay
        },
        { id: done.outputAssetId, revision: rendered.revision },
      ),
    )
  })

  await s.guard('pause', async () => {
    await lib(page, 'SetPaused', true)
    const source = named('scan.png')
    const run = await compression(
      page,
      'Plan',
      [{ assetId: source.id, path: '' }],
      '',
      options(),
    )
    await compression(page, 'Analyse', run.id)
    await sleep(300)
    await compression(page, 'Pause', run.id)
    const jobs = await settled(page, run.id, ['paused'])
    s.check(
      'paused indexing prevents encoding and publication',
      jobs[0].state === 'planned' && !existsSync(jobs[0].to),
      jobs,
    )
    await compression(page, 'Cancel', run.id)
    await lib(page, 'SetPaused', false)
  })

  await s.guard('settings', async () => {
    const prefs = await lib(page, 'Preferences')
    await lib(page, 'SetPreferences', {
      ...prefs,
      compression: options('reversible', 'maximum', 7),
    })
    await close(app)
    app = undefined
    page = await start()
    s.check(
      'compression defaults survive restart',
      (await lib(page, 'Preferences')).compression.mode === 'reversible',
    )
    await lib(page, 'ResetPreferences', ['compression'])
    s.check(
      'compression reset restores safe compatible defaults',
      (await lib(page, 'Preferences')).compression.mode === 'compatible',
    )
    await go(page, '/settings')
    s.check(
      'Compression appears in Settings',
      await page.evaluate(() =>
        document.body.textContent.includes('Minimum size reduction'),
      ),
    )
    await go(page, '/compression')
    for (const width of [1000, 1440]) {
      await resize(page, width, 850)
      s.check(
        `tools workspace fits ${width}px`,
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      )
    }
  })
  for (const [name, before] of originals)
    s.check(`original retained: ${name}`, hash(join(photos, name)) === before)
  s.check('no off-device request', external.length === 0, external)
  s.check('no uncaught UI errors', s.appErrors().length === 0, s.appErrors())
} catch (error) {
  s.check('suite completed', false, String(error))
} finally {
  if (app) await close(app)
  s.finish()
}
