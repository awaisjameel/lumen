// The viewer and what is done from it, in the native WebView2: zoom, pan,
// rotate, full screen, filmstrip, compare, slideshow and auto-hide; selection
// and batch actions; export and rename with Undo; audio tracks, subtitles and
// frame export of videos; playback history; and what a block takes away.
//   node suites/viewer.mjs
import { existsSync, mkdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { call, close, launch, lib, resize, sleep } from '../lib/app.mjs'
import { fixtureCopy } from '../lib/fixtures.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('viewer')
const { check, errors } = s
const root = s.out
const destination = join(s.run, 'exports')
mkdirSync(destination, { recursive: true })
// Its own copy of the files: the suite renames one and puts it back.
const source = fixtureCopy('media', join(s.run, 'source'))
const externalRequests = []
const app = await launch({ exe: s.exe, dataDir: join(s.run, 'data'), port: s.port })
const page = app.page
page.on('pageerror', (e) => errors.push(String(e)))
page.on('request', (request) => {
  const url = new URL(request.url())
  if (
    ['http:', 'https:'].includes(url.protocol) &&
    !['wails.localhost', 'localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  )
    externalRequests.push(url.href)
})
const media = (method, ...args) => call(page, `lumen/services.MediaService.${method}`, ...args)
const clickLabel = async (label) => {
  await page.click(`button[aria-label="${label}"]`)
  await sleep(250)
}
const clickText = async (text) => {
  const button = await page.evaluateHandle(
    (text) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === text),
    text,
  )
  if (!button.asElement()) throw Error(`Missing ${text}`)
  await button.asElement().click()
  await sleep(250)
}
// Opens the menu behind a labelled button and picks one of its items.
const menuItem = async (label, text) => {
  await page.click(`button[aria-label="${label}"]`)
  await page.waitForSelector('[role="menuitem"]')
  await page.evaluate(
    (text) => [...document.querySelectorAll('[role="menuitem"]')].find((e) => e.textContent.trim().startsWith(text))?.click(),
    text,
  )
  await sleep(250)
}
const dialogAction = async (text) => {
  const button = await page.evaluateHandle((text) => {
    const dialog = [...document.querySelectorAll('[role=dialog]')].at(-1)
    return dialog && [...dialog.querySelectorAll('button')].find((b) => b.textContent.trim() === text)
  }, text)
  if (!button.asElement()) throw Error(`Missing dialog action ${text}`)
  await button.asElement().click()
  await sleep(250)
}
const view = { filter: '', tag: 0, person: 0, album: 0, place: 0, event: 0 }
try {
  await resize(page, 1440, 900)
  await lib(page, 'SetPauseBusy', false)
  await lib(page, 'SetIdleOnly', false)
  await lib(page, 'SetVideos', true)
  const sources = await lib(page, 'Sources')
  if (!sources.length) await lib(page, 'AddSources', [source])
  for (let i = 0; i < 180; i++) {
    const progress = await lib(page, 'Progress')
    if (progress.total >= 16 && !progress.scanning && !progress.metadata && !progress.thumbnail) break
    await sleep(500)
  }
  await lib(page, 'SetPaused', true)
  const prefs = await lib(page, 'Preferences')
  await lib(page, 'SetPreferences', {
    ...prefs,
    viewer: { ...prefs.viewer, filmstrip: false, autoplay: false, autoHide: true, slideshowSeconds: 1 },
  })
  await page.reload()
  await sleep(1200)
  const selection = await lib(page, 'Selection', view, [], false)
  const details = await Promise.all(selection.ids.map((id) => lib(page, 'Asset', id)))
  const photo = details.find((d) => d.paths.some((p) => p.endsWith('photo-01.jpg')))
  const tiff = details.find((d) => d.ext === '.tiff'),
    heic = details.find((d) => d.ext === '.heic')
  const oriented = details.find((d) => d.paths.some((p) => p.endsWith('photo-00.jpg')))
  const p3 = details.find((d) => d.paths.some((p) => p.endsWith('red-p3.jpg')))
  const direct = details.find((d) => d.ext === '.mp4'),
    converted = details.find((d) => d.ext === '.mkv')
  check('isolated library indexed all fixtures', details.length >= 16, details.length)
  check(
    'render capability matches installed runtime',
    details.filter((d) => d.kind === 'photo').every((d) => d.caps.render),
  )
  await lib(page, 'SetAdjustment', photo.id, 0, false)
  const orientedInfo = await media('Inspect', oriented.id)
  const upright = await media('Render', oriented.id, 4096)
  check(
    'EXIF orientation is applied once',
    orientedInfo.orientation === 6 &&
      upright.width === oriented.width &&
      upright.height === oriented.height &&
      upright.height > upright.width,
    { orientedInfo, upright },
  )
  const p3Info = await media('Inspect', p3.id)
  check(
    'embedded colour profile is reported by name',
    p3Info.hasProfile === true && p3Info.profile.length > 0,
    p3Info.profile,
  )
  for (const d of [tiff, heic].filter(Boolean)) {
    const render = await media('Render', d.id, 4096)
    const result = await page.evaluate(
      async ([id, revision]) => {
        const response = await fetch(`/media/render/${id}/4096?v=${revision}`)
        const image = new Image()
        image.src = URL.createObjectURL(await response.blob())
        await image.decode()
        return { status: response.status, w: image.naturalWidth, h: image.naturalHeight }
      },
      [d.id, render.revision],
    )
    check(`native viewing render ${d.ext}`, result.status === 200 && result.w > 1024, result)
    if (d === tiff)
      check(
        'large render reports honest resolution limit',
        render.limited && render.width === 4096 && d.width === 6000,
        render,
      )
  }
  const first = await page.$(`#cell-${photo.id}`)
  await first.click({ count: 2 })
  await page.waitForSelector('.viewer .image-surface .original')
  await sleep(500)
  const fitted = await page.$eval('.image-controls [role=status]', (e) => e.textContent)
  await clickLabel('Zoom level')
  await clickText('Actual size')
  check(
    'actual size and effective zoom',
    (await page.$eval('.image-controls [role=status]', (e) => e.textContent)) === '100%',
    fitted,
  )
  const bounds = await page.$eval('.image-surface', (e) => {
    const r = e.getBoundingClientRect()
    return { x: r.x, y: r.y, w: r.width, h: r.height }
  })
  await page.mouse.move(bounds.x + bounds.w * 0.7, bounds.y + bounds.h * 0.6)
  await page.mouse.wheel({ deltaY: -80 })
  await sleep(250)
  check(
    'pointer-centred wheel zoom',
    await page.$eval('.image-controls [role=status]', (e) => parseInt(e.textContent) > 100),
  )
  const beforePan = await page.$eval('.image-surface .original', (e) => e.style.transform)
  await page.mouse.move(bounds.x + bounds.w * 0.5, bounds.y + bounds.h * 0.5)
  await page.mouse.down()
  await page.mouse.move(bounds.x + bounds.w * 0.5 + 100, bounds.y + bounds.h * 0.5 + 40, { steps: 5 })
  await page.mouse.up()
  check(
    'native drag pans source pixels',
    await page.$eval('.image-surface .original', (e, before) => e.style.transform !== before, beforePan),
  )
  const beforeKey = await page.$eval('.image-controls [role=status]', (e) => parseInt(e.textContent))
  await page.keyboard.press('+')
  await sleep(150)
  check(
    'plus key zoom uses the shared command',
    await page.$eval('.image-controls [role=status]', (e, n) => parseInt(e.textContent) > n, beforeKey),
  )
  // Fit to window lives in the zoom level's menu.
  await clickLabel('Zoom level')
  await page.evaluate(() =>
    [...document.querySelectorAll('[role="menuitem"]')].find((e) => e.textContent.trim() === 'Fit to window')?.click(),
  )
  await sleep(250)
  await clickLabel('Rotate clockwise')
  const adjusted = await lib(page, 'Asset', photo.id)
  await page.waitForFunction(() => {
    const image = document.querySelector('.image-surface .original')
    return image?.complete && image.naturalWidth > 0 && getComputedStyle(image).display !== 'none'
  })
  check('presentation refresh keeps the loaded original visible', true)
  check(
    'display rotation persists without source changes',
    adjusted.adjustment.rotation === 1 && adjusted.width === 1800 && adjusted.height === 1200,
    adjusted.adjustment,
  )
  await clickLabel('Fullscreen')
  await page.waitForFunction(() => !!document.fullscreenElement)
  check('image viewer native fullscreen enters', true)
  await page.hover('.viewer button[aria-label="Close"]')
  await page.waitForFunction(() => document.fullscreenElement?.querySelector('[role="tooltip"]'), { timeout: 5000 })
  check('image fullscreen retains visible tooltips inside its native tree', true)
  await clickLabel('Exit fullscreen')
  await page.waitForFunction(() => !document.fullscreenElement)
  check('image viewer native fullscreen exits', true)
  await clickLabel('Filmstrip')
  await page.waitForSelector('.filmstrip button')
  check('virtual filmstrip has bounded neighbour DOM', (await page.$$('.filmstrip button')).length <= 25)
  await clickLabel('Compare images')
  await page.waitForFunction(() => document.querySelectorAll('.viewer .image-surface').length === 2)
  check('comparison has two independent surfaces', true)
  await clickLabel('Zoom in')
  const transforms = await page.$$eval('.image-surface .original', (els) => els.map((e) => e.style.transform))
  check(
    'linked comparison applies geometry',
    transforms.length === 2 && transforms.every((t) => t.includes('scale(')),
    transforms,
  )
  await page.screenshot({ path: join(root, 'viewer.png') })
  await clickLabel('Compare images')
  await clickLabel('Reset display adjustments')
  await clickLabel('Close')
  const slidePhoto = details.find((d) => d.kind === 'photo' && d.ext === '.jpg' && d.id !== selection.ids.at(-1))
  await page.click(`#cell-${slidePhoto.id}`, { count: 2 })
  await page.waitForSelector('.filmstrip button')
  check('saved filmstrip loads when reopening viewer', true)
  await page.waitForSelector('.viewer .original')
  const beforeSlide = await page.$eval('.viewer .original', (e) => e.src)
  await clickLabel('Slideshow')
  await page.waitForFunction(
    (before) => document.querySelector('.viewer .original')?.src !== before,
    { timeout: 5000 },
    beforeSlide,
  )
  await clickLabel('Slideshow')
  check('slideshow advances and can pause', true)
  // The next slideshow entry can be a video, whose player owns its controls.
  // Verify image auto-hide with a known photo rather than the random next item.
  await clickLabel('Close')
  await page.click(`#cell-${photo.id}`, { count: 2 })
  await page.waitForSelector('.viewer .image-surface .original')
  await page.$eval('.viewer', (e) => e.focus())
  await page.waitForSelector('.viewer.controls-hidden', { timeout: 5000 })
  check('auto-hide starts without further pointer input', true)
  await page.mouse.move(900, 300)
  await page.waitForFunction(() => !document.querySelector('.viewer').classList.contains('controls-hidden'))
  check('pointer activity restores controls', true)
  await clickLabel('Close')
  const cells = await page.$$('.scroller .cell[role=option]')
  await cells[0].click()
  await page.keyboard.down('Control')
  await cells[1].click()
  await page.keyboard.up('Control')
  check('Ctrl-click keeps two selections', (await page.$$('.cell[aria-selected=true]')).length === 2)
  await menuItem('Selection options', 'Select all results')
  await page.waitForFunction(
    (n) => document.querySelector('.selection-dock [aria-live]')?.textContent.includes(String(n)),
    { timeout: 5000 },
    details.length,
  )
  check(
    'select-all uses honest count',
    await page.$eval('.selection-dock [aria-live]', (e, n) => e.textContent.includes(String(n)), details.length),
  )
  await clickLabel('Clear selection')
  const batchCells = await page.$$('.scroller .cell[role=option]')
  await batchCells[0].click()
  await page.keyboard.down('Control')
  await batchCells[1].click()
  await page.keyboard.up('Control')
  await menuItem('More actions', 'Selection actions')
  await page.waitForSelector('[role=dialog] [role=combobox]')
  // The apply button names the chosen action; adding a favourite needs no
  // confirmation (only deleting from disk asks).
  await page.click('[role=dialog] [data-batch-apply]')
  await page.waitForFunction(
    () =>
      [...document.querySelectorAll('[role=dialog] [role=status]')].some((e) =>
        e.textContent.includes('2 of 2 processed; 0 failed'),
      ),
    { timeout: 10000 },
  )
  check('native bulk action reports per-item success and stable count', true)
  await page.keyboard.press('Escape')
  await clickLabel('Clear selection')
  const exportPlan = await lib(page, 'PlanExport', [photo.id], destination, {
    format: 'png',
    side: 2048,
    quality: 90,
    adjust: true,
    frame: -1,
  })
  check('export batch ID survives JavaScript bridge', Number.isSafeInteger(exportPlan[0].batch), exportPlan[0].batch)
  const exported = await lib(page, 'RunExport', exportPlan[0].batch)
  check(
    'native rendered export publishes new copy',
    exported[0].state === 'done' && existsSync(exported[0].to),
    exported[0].state,
  )
  const originalPlan = await lib(page, 'PlanExport', [photo.id], destination, {
    format: 'original',
    side: 4096,
    quality: 92,
    adjust: false,
    frame: -1,
  })
  const copied = await lib(page, 'RunExport', originalPlan[0].batch)
  check(
    'original copy retains exact bytes',
    copied[0].state === 'done' && readFileSync(copied[0].from).equals(readFileSync(copied[0].to)),
  )
  const rename = await lib(page, 'PlanFileMoves', [photo.id], '', 'renamed.jpg')
  await lib(page, 'StartOrganise', rename.runId)
  for (let i = 0; i < 100; i++) {
    const runs = await lib(page, 'OrganiseRuns')
    if (runs.find((r) => r.id === rename.runId)?.state === 'done') break
    await sleep(100)
  }
  check(
    'rename preserves asset identity',
    (await lib(page, 'Asset', photo.id)).paths.some((p) => p.endsWith('renamed.jpg')),
  )
  await lib(page, 'UndoOrganise', rename.runId)
  for (let i = 0; i < 100; i++) {
    const d = await lib(page, 'Asset', photo.id)
    if (d.paths.some((p) => p.endsWith('photo-01.jpg'))) break
    await sleep(100)
  }
  check(
    'rename Undo restores the original path',
    (await lib(page, 'Asset', photo.id)).paths.some((p) => p.endsWith('photo-01.jpg')),
  )
  const snapshot = await lib(page, 'Selection', view, [], false)
  await lib(page, 'SetFavourite', photo.id, !(await lib(page, 'Asset', photo.id)).favourite)
  let refused = false
  try {
    await lib(page, 'ValidateSelection', snapshot)
  } catch {
    refused = true
  }
  check('stale result snapshot is refused', refused)
  for (const d of [direct, converted]) {
    const info = await media('Inspect', d.id)
    check(
      `track discovery ${d.ext}`,
      info.tracks.filter((t) => t.kind === 'audio').length === 2 &&
        info.tracks.some((t) => t.kind === 'subtitle' && t.supported),
      info.tracks,
    )
    await page.reload()
    await sleep(600)
    await page.click(`#cell-${d.id}`, { count: 2 })
    await page.waitForSelector('video')
    await page.waitForFunction(() => document.querySelector('video')?.readyState >= 2, { timeout: 30000 })
    check(`native playback ready ${d.ext}`, true)
    await clickLabel('Playback settings')
    await page.waitForSelector('select[aria-label="Audio track (uses conversion)"]', { timeout: 15000 })
    await page.select('select[aria-label="Audio track (uses conversion)"]', '2')
    await page.waitForFunction(
      () =>
        document.querySelector('video')?.src.includes('audio=2') && document.querySelector('video')?.readyState >= 2,
      { timeout: 30000 },
    )
    check(`native audio switch ${d.ext}`, await page.$eval('video', (v) => v.src.includes('audio=2')))
    await page.select('select[aria-label="Subtitle track"]', '3')
    await page.waitForFunction(
      () => [...document.querySelector('video').textTracks].some((t) => t.mode === 'showing' && t.cues?.length),
      { timeout: 30000 },
    )
    check(`native text subtitles ${d.ext}`, true)
    const initialCues = await page.$eval(
      'video',
      (v) => [...v.textTracks].find((t) => t.mode === 'showing').cues.length,
    )
    for (let i = 0; i < 3; i++) {
      await page.select('select[aria-label="Subtitle track"]', '-1')
      await sleep(100)
      await page.select('select[aria-label="Subtitle track"]', '3')
      await page.waitForFunction(
        () => [...document.querySelector('video').textTracks].some((t) => t.mode === 'showing' && t.cues?.length),
        { timeout: 30000 },
      )
    }
    check(
      `subtitle switches reuse one cue track ${d.ext}`,
      await page.$eval(
        'video',
        (v, count) =>
          v.textTracks.length === 1 && [...v.textTracks].find((t) => t.mode === 'showing').cues.length === count,
        initialCues,
      ),
    )
    await clickLabel('Close playback settings')
    await clickLabel('Play')
    await page.waitForFunction(() => document.querySelector('video').currentTime > 0.25, { timeout: 10000 })
    check(
      `native subtitle cues activate during playback ${d.ext}`,
      await page.$eval('video', (v) => [...v.textTracks].some((t) => t.mode === 'showing' && t.activeCues?.length > 0)),
    )
    await clickLabel('Pause')
    await page.screenshot({ path: join(root, `video-${d.ext.slice(1)}.png`) })
    if (d === direct) {
      await page.click('.video-player button[aria-label="Fullscreen"]')
      await page.waitForFunction(() => document.fullscreenElement?.classList.contains('video-player'))
      check('video native fullscreen enters before frame saving', true)
    }
    // The controls settle after fullscreen and fade when idle: wake them and
    // click once the button itself takes the pointer. The viewer is a dialog
    // too, so wait for the export dialog's own field.
    const player = await (await page.$('.video-player')).boundingBox()
    await page.mouse.move(player.x + player.width / 2, player.y + player.height / 2)
    await page.waitForFunction(
      () => {
        const b = document.querySelector('.video-player button[aria-label="Save current frame"]'),
          r = b?.getBoundingClientRect()
        return !!r && b.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2))
      },
      { timeout: 5000 },
    )
    await clickLabel('Save current frame')
    await page.waitForSelector('input[aria-label="Destination folder"]')
    if (d === direct)
      check(
        'fullscreen frame dialog is inside the visible native tree',
        await page.evaluate(() => !!document.fullscreenElement?.querySelector('[role="dialog"][data-state="open"]')),
      )
    check(
      `frame export opens explicit destination UI ${d.ext}`,
      await page.$$eval('[role=dialog]', (els) =>
        els.some(
          (e) => e.textContent.includes('Export JPEG') && !!e.querySelector('input[aria-label="Destination folder"]'),
        ),
      ),
      await page.$$eval('[role=dialog]', (els) => els.map((e) => e.textContent.slice(-500))),
    )
    const frameDestination = join(destination, `frame-ui-${d.ext.slice(1)}`)
    mkdirSync(frameDestination, { recursive: true })
    await page.type('input[aria-label="Destination folder"]', frameDestination)
    await dialogAction('Preview paths')
    await page.waitForFunction(() => document.querySelector('.outcomes')?.textContent.includes('Planned'), {
      timeout: 10000,
    })
    // The preview is the confirmation: its commit button runs the export.
    if (d === direct) {
      check(
        'fullscreen export action is inside the visible native tree',
        await page.evaluate(() => !!document.fullscreenElement?.querySelector('button[data-files-commit]')),
      )
      await page.screenshot({ path: join(root, 'fullscreen-confirmation.png') })
    }
    await page.click('button[data-files-commit]')
    await page.waitForFunction(() => document.querySelector('.outcomes')?.textContent.includes('Done'), {
      timeout: 20000,
    })
    check(
      `native frame UI publishes its confirmed export ${d.ext}`,
      existsSync(join(frameDestination, 'tracks-frame.jpg')),
    )
    await page.waitForSelector('button[data-files-commit]', { hidden: true })
    await page.click('[role="dialog"][data-state="open"] button[aria-label="Close"]')
    await page.waitForSelector('.outcomes', { hidden: true })
    if (d === direct) {
      // The dialog fades out over the player: a click before it has gone lands on it.
      await page.waitForFunction(
        () => {
          const b = document.querySelector('.video-player button[aria-label="Exit fullscreen"]'),
            r = b?.getBoundingClientRect()
          return !!r && b.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2))
        },
        { timeout: 5000 },
      )
      await page.click('.video-player button[aria-label="Exit fullscreen"]')
      await page.waitForFunction(() => !document.fullscreenElement)
      check('video fullscreen exits after its confirmed export', true)
    }
    await clickLabel('Close')
  }
  const frame = await lib(page, 'PlanExport', [direct.id], destination, {
    format: 'jpeg',
    side: 1024,
    quality: 90,
    adjust: false,
    frame: 1500,
  })
  const saved = await lib(page, 'RunExport', frame[0].batch)
  check('native source-time frame export', saved[0].state === 'done' && existsSync(saved[0].to))
  await lib(page, 'SetPlaybackPosition', direct.id, 1000)
  check('resume stays off by default', (await lib(page, 'PlaybackPosition', direct.id)) === 0)
  const playbackPrefs = await lib(page, 'Preferences')
  await lib(page, 'SetPreferences', { ...playbackPrefs, viewer: { ...playbackPrefs.viewer, resume: true } })
  await lib(page, 'SetPlaybackPosition', direct.id, 1500)
  check('resume history persists only when opted in', (await lib(page, 'PlaybackPosition', direct.id)) === 1500)
  await lib(page, 'ClearPlaybackHistory')
  check('clear playback history forgets positions', (await lib(page, 'PlaybackPosition', direct.id)) === 0)
  await lib(page, 'CreateBlockPIN', '123456')
  await lib(page, 'SetBlock', 'asset', photo.id, '', 'photos')
  const protectedBatch = await lib(page, 'ApplyBatch', [photo.id], 'favourite', 0)
  check(
    'batch editing refuses an asset covered after selection',
    protectedBatch.length === 1 && protectedBatch[0].code === 'not_found',
    protectedBatch,
  )
  const history = await lib(page, 'ExportJobs', 0)
  check(
    'export history hides blocked asset paths',
    history.every((j) => j.assetId !== photo.id),
  )
  let hidden = false
  try {
    await media('Render', photo.id, 4096)
  } catch {
    hidden = true
  }
  check('blocked render RPC is refused', hidden)
  await page.reload()
  await sleep(600)
  await page.click(`#cell-${direct.id}`, { count: 2 })
  await page.waitForSelector('video')
  await page.waitForFunction(() => document.querySelector('video')?.readyState >= 2, { timeout: 30000 })
  await lib(page, 'SetBlock', 'asset', direct.id, '', 'photos')
  await page.waitForFunction(() => !document.querySelector('.viewer video'), { timeout: 10000 })
  check('hiding the active video removes playback', true)
  let historyDenied = false
  try {
    await lib(page, 'SetPlaybackPosition', direct.id, 1500)
  } catch {
    historyDenied = true
  }
  check('covered media cannot record resume history', historyDenied)
  check('no uncaught native frontend errors', errors.length === 0, errors)
  check('native workflows make no external requests', externalRequests.length === 0, externalRequests)
} catch (e) {
  check('native acceptance completed', false, String(e))
  console.error(e)
  await page.screenshot({ path: join(root, 'failure.png') })
  console.log(await page.evaluate(() => document.body.innerText.slice(-1600)))
} finally {
  await close(app)
  s.finish({ externalRequests })
}
