// Customisation in the native WebView2: every preference changed where a
// person changes it and checked where it takes effect (the page, the grid,
// the viewer, the scanner), then through a restart; reassigned shortcuts,
// format exclusion on real files, the viewing cache, export presets and reset.
// SECTIONS=shortcuts,formats runs only those sections.
//   node suites/customise.mjs
import { execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { close, go, launch, lib, media, resize, sleep, unthrottle } from '../lib/app.mjs'
import { libraryCopy } from '../lib/fixtures.mjs'
import { FFMPEG, suite } from '../lib/suite.mjs'

const s = suite('customise')
const { check, guard } = s
const dataDir = await libraryCopy(join(s.run, 'data'))

// A few files of its own, in formats the base library does not hold.
const extra = join(s.run, 'extra')
mkdirSync(extra, { recursive: true })
const ffmpeg = (...args) => execFileSync(FFMPEG, ['-hide_banner', '-loglevel', 'error', '-y', ...args])
const still = (name) => ffmpeg('-f', 'lavfi', '-i', 'testsrc2=size=320x240', '-frames:v', '1', join(extra, name))
still('first.png')
still('plain.jpg')
ffmpeg('-f', 'lavfi', '-i', 'testsrc2=size=160x120:rate=5', '-t', '1', join(extra, 'spinner.gif'))

const external = []
let app
let page
async function start() {
  app = await launch({ exe: s.exe, dataDir, port: s.port })
  page = app.page
  s.watch(page)
  page.on('request', (r) => {
    const host = new URL(r.url()).hostname
    if (
      !['wails.localhost', '127.0.0.1', 'localhost', ''].includes(host) &&
      !r.url().startsWith('data:') &&
      !r.url().startsWith('blob:')
    )
      external.push(r.url().slice(0, 200))
  })
  await resize(page, 1440, 900)
  await page.waitForSelector('.shell, .scroller', { timeout: 60000 })
}
const restart = async () => {
  await close(app)
  await start()
}
const prefs = () => lib(page, 'Preferences')
const root = (name) => page.evaluate((name) => document.documentElement.dataset[name] ?? null, name)
const settings = async (category = '') => {
  await go(page, '/settings', 700)
  await page.waitForSelector('[data-category="appearance"]')
  if (category) {
    await page.evaluate((category) => {
      const section = document.querySelector(`[data-category="${category}"]`)
      section?.scrollIntoView({ block: 'start', behavior: 'instant' })
    }, category)
    await sleep(200)
  }
}
/** Clicks the nth choice of a radio group, found by its data hook. */
const choose = async (hook, index) => {
  const radios = await page.$$(`${hook} [role="radio"]`)
  if (!radios[index]) throw new Error(`no choice ${index} in ${hook}`)
  await radios[index].evaluate((el) => el.scrollIntoView({ block: 'center', behavior: 'instant' }))
  await sleep(250)
  await radios[index].click()
  await sleep(350)
}
const click = async (selector) => {
  const el = await page.waitForSelector(selector, { timeout: 10000 })
  await el.evaluate((e) => e.scrollIntoView({ block: 'center', behavior: 'instant' }))
  await sleep(250)
  await el.click()
  await sleep(350)
}
/** Answers the confirmation that is showing with its confirming button. */
const confirm = async (label) => {
  await page.waitForFunction(
    (label) =>
      [...document.querySelectorAll('[role="dialog"] button, [role="alertdialog"] button')].some(
        (b) => b.textContent.trim() === label,
      ),
    { timeout: 10000 },
    label,
  )
  await page.evaluate((label) => {
    const button = [...document.querySelectorAll('[role="dialog"] button, [role="alertdialog"] button')].find(
      (b) => b.textContent.trim() === label,
    )
    button.click()
  }, label)
  await sleep(600)
}
const openViewer = async (id) => {
  await page.waitForSelector(`#cell-${id}`, { timeout: 20000 })
  await page.$eval(`#cell-${id}`, (cell) => cell.scrollIntoView({ block: 'center' }))
  await page.click(`#cell-${id}`, { count: 2 })
  await page.waitForSelector('[aria-modal="true"].viewer', { timeout: 10000 })
  await sleep(900)
}
const closeViewer = async () => {
  await page.keyboard.press('Escape')
  await page.waitForFunction(() => !document.querySelector('[aria-modal="true"].viewer'), { timeout: 10000 })
  await sleep(300)
}
const waitScanned = async (total) => {
  const started = Date.now()
  for (;;) {
    const p = await lib(page, 'Progress')
    if (!p.scanning && p.metadata === 0 && p.thumbnail === 0 && (total === undefined || p.total === total)) return p
    if (Date.now() - started > 120_000) throw new Error(`the scan did not settle: ${JSON.stringify(p)}`)
    await sleep(500)
  }
}
/** The grid's first loaded photo (the newest day's first). */
const firstPhoto = async () => {
  const days = await lib(page, 'Timeline', { filter: 'all' })
  const day = days[0].day
  const [items] = await lib(page, 'Days', day, day, { filter: 'all' })
  return items.items.find((it) => !it.v)
}

await start()
const defaults = await prefs()

await guard('sections', async () => {
  await settings()
  const sections = await page.$$eval('[data-category]', (els) => els.map((e) => e.dataset.category))
  check(
    'Settings shows every section of the plan',
    [
      'appearance',
      'library',
      'viewing',
      'video',
      'indexing',
      'search',
      'files',
      'accessibility',
      'privacy',
      'presets',
      'about',
    ].every((id) => sections.includes(id)),
    sections.join(','),
  )
  await page.type('input[type="search"]', 'shortcut')
  await sleep(400)
  const shown = await page.$$eval('[data-category]', (els) =>
    els.filter((e) => e.getBoundingClientRect().height > 0).map((e) => e.dataset.category),
  )
  check(
    'searching for "shortcut" leaves only the sections that mention shortcuts',
    shown.includes('accessibility') && !shown.includes('appearance') && !shown.includes('indexing'),
    shown.join(','),
  )
  await page.screenshot({ path: join(s.out, 'shortcuts.png') })
  await page.$eval('input[type="search"]', (input) => {
    input.value = ''
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await sleep(300)
})

await guard('appearance', async () => {
  await settings('appearance')
  await choose('[data-appearance="motion"]', 1)
  const duration = await page.evaluate(() => {
    const probe = document.createElement('div')
    probe.style.transition = 'opacity 2s'
    document.body.append(probe)
    const value = getComputedStyle(probe).transitionDuration
    probe.remove()
    return value
  })
  check(
    'Reduced motion holds every transition still',
    (await root('motion')) === 'reduced' && parseFloat(duration) < 0.001,
    `data-motion=${await root('motion')} transition=${duration}`,
  )
  await choose('[data-appearance="motion"]', 2)
  const full = await page.evaluate(() => {
    const probe = document.createElement('div')
    probe.style.transition = 'opacity 2s'
    document.body.append(probe)
    const value = getComputedStyle(probe).transitionDuration
    probe.remove()
    return value
  })
  check('Full motion lets transitions run', (await root('motion')) === 'full' && full === '2s', full)

  await choose('[data-appearance="viewerBackground"]', 1)
  await choose('[data-accessibility="focusRing"]', 1)
  const stored = await prefs()
  check(
    'motion, viewer background and focus ring are saved in the library',
    stored.motion === 'full' && stored.viewerBackground === 'black' && stored.focusRing === 'strong',
    JSON.stringify({ motion: stored.motion, bg: stored.viewerBackground, focus: stored.focusRing }),
  )
  await page.keyboard.press('Tab')
  const ring = await page.evaluate(() => {
    const el = document.activeElement
    const style = getComputedStyle(el)
    return { width: style.outlineWidth, focus: document.documentElement.dataset.focus, tag: el?.tagName }
  })
  check(
    'the strong focus ring is drawn thicker on the focused control',
    ring.focus === 'strong' && parseFloat(ring.width) > 2,
    ring,
  )

  await go(page, '/', 800)
  const photo = await firstPhoto()
  await openViewer(photo.id)
  const background = await page.$eval('[aria-modal="true"].viewer', (el) => getComputedStyle(el).backgroundColor)
  check('the viewer is drawn on the black background chosen', background === 'rgb(0, 0, 0)', background)
  await page.screenshot({ path: join(s.out, 'viewer-black.png') })
  await closeViewer()
})

await guard('library', async () => {
  await settings('library')
  await choose('[data-library="thumbFit"]', 1)
  await click('[data-library="badge-favourite"]')
  await go(page, '/favourites', 900)
  await page.waitForSelector('.scroller .cell img')
  const tile = await page.evaluate(() => {
    const img = document.querySelector('.scroller .cell .media-tile img')
    const badge = document.querySelector('.scroller .cell .badge.fav')
    return {
      fit: img ? getComputedStyle(img).objectFit : null,
      badge: badge ? getComputedStyle(badge).display : 'absent',
    }
  })
  check('thumbnails show the whole picture once that is chosen', tile.fit === 'contain', tile)
  check('the favourite badge is hidden on favourites, which stay favourites', tile.badge === 'none', tile)
  check('hiding the badge changed no favourite', (await lib(page, 'Timeline', { filter: 'favourites' })).length > 0)
  await page.screenshot({ path: join(s.out, 'grid-contain.png') })

  await settings('library')
  await choose('[data-library="thumbFit"]', 0)
  await click('[data-library="badge-favourite"]')
  const back = await prefs()
  check('the thumbnail choices go back as they were set', back.thumbFit === 'cover' && !back.hideFavouriteBadge)
})

await guard('start view', async () => {
  const p = await prefs()
  await lib(page, 'SetPreferences', { ...p, startView: 'albums' })
  await restart()
  await page.waitForFunction(() => location.pathname === '/albums', { timeout: 20000 }).catch(() => {})
  check('Lumen opens on the screen chosen', (await page.evaluate(() => location.pathname)) === '/albums')
  const kept = await prefs()
  check(
    'every preference set so far survived the restart',
    kept.startView === 'albums' &&
      kept.motion === 'full' &&
      kept.viewerBackground === 'black' &&
      kept.focusRing === 'strong',
    JSON.stringify({ start: kept.startView, motion: kept.motion, bg: kept.viewerBackground, focus: kept.focusRing }),
  )
  check(
    'the restored preferences are applied to the page again',
    (await root('viewerBg')) === 'black' && (await root('focus')) === 'strong',
  )
})

await guard('shortcuts', async () => {
  await settings('accessibility')
  const row = (id) => `[data-shortcut="${id}"]`
  const record = async (id, key) => {
    await click(`${row(id)} [data-shortcut-change]`)
    await page.keyboard.press(key)
    await sleep(500)
  }
  await record('viewer.rotate', 'r')
  check(
    'a key pressed in the editor becomes the action’s shortcut',
    (await prefs()).shortcuts?.['viewer.rotate'] === 'R',
    JSON.stringify((await prefs()).shortcuts),
  )

  await record('viewer.flip', 'f')
  const refused = await page
    .$eval(`${row('viewer.flip')} [role="alert"]`, (el) => el.textContent.trim())
    .catch(() => '')
  check(
    'keys another viewer action answers to are refused, with whose they are',
    /already belongs/.test(refused) && (await prefs()).shortcuts?.['viewer.flip'] === undefined,
    refused,
  )
  check(
    'Close keeps Escape and offers no change',
    (await page.$(`${row('viewer.close')} [data-shortcut-change]`)) === null,
  )
  await page.screenshot({ path: join(s.out, 'shortcut-editor.png') })

  await go(page, '/', 800)
  const photo = await firstPhoto()
  await openViewer(photo.id)
  const before = (await lib(page, 'Asset', photo.id)).adjustment?.rotation ?? 0
  await page.keyboard.press('r')
  await sleep(900)
  const after = (await lib(page, 'Asset', photo.id)).adjustment?.rotation ?? 0
  check('the new shortcut runs its action in the viewer', after === (before + 1) % 4, `${before} -> ${after}`)
  await lib(page, 'SetAdjustment', photo.id, 0, false)
  await page.keyboard.press('Escape')
  await sleep(500)
  check('Escape still closes the viewer', (await page.$('[aria-modal="true"].viewer')) === null)

  // The shortcut reference lists what is in force.
  await page.keyboard.press('?')
  await sleep(600)
  const help = await page.evaluate(() => document.body.innerText)
  await page.keyboard.press('Escape')
  await sleep(300)
  check('the shortcut reference opens with its own key', /Keyboard shortcuts/.test(help))

  await settings('accessibility')
  await click('[data-shortcut-reset-all]')
  check('Reset all shortcuts clears every change', Object.keys((await prefs()).shortcuts ?? {}).length === 0)
})

await guard('viewing', async () => {
  await settings('viewing')
  await choose('[data-viewing="open"]', 1)
  await choose('[data-viewing="wheel"]', 1)
  const set = await prefs()
  check(
    'how photos open and what the wheel does are saved',
    set.viewer.open === 'actual' && set.viewer.wheel === 'navigate',
    JSON.stringify(set.viewer),
  )

  await go(page, '/', 800)
  const photo = await firstPhoto()
  await openViewer(photo.id)
  const scale = await page.$eval('.image-controls [role="status"]', (el) => el.textContent.trim())
  check('a photo opens at its actual size', scale === '100%', scale)
  const label = () => page.$eval('[aria-modal="true"].viewer', (el) => el.getAttribute('aria-label'))
  const first = await page.evaluate(() => document.querySelector('.image-surface img')?.getAttribute('src') ?? '')
  const box = await (await page.$('.image-surface')).boundingBox()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await page.mouse.wheel({ deltaY: 120 })
  await sleep(1200)
  const second = await page.evaluate(() => document.querySelector('.image-surface img')?.getAttribute('src') ?? '')
  const stillActual = await page.$eval('.image-controls [role="status"]', (el) => el.textContent.trim())
  check(
    'the wheel moves to the next photo instead of zooming',
    first !== second && stillActual === '100%',
    `${first} -> ${second} at ${stillActual} (${await label()})`,
  )
  await closeViewer()
  await settings('viewing')
  await choose('[data-viewing="open"]', 0)
  await choose('[data-viewing="wheel"]', 0)
  const back = await prefs()
  check('the viewing choices go back as they were set', back.viewer.open === 'fit' && back.viewer.wheel === 'zoom')
})

let total = 0
await guard('formats', async () => {
  await unthrottle(page)
  const base = (await waitScanned()).total
  await lib(page, 'AddSources', [extra])
  total = (await waitScanned(base + 3)).total
  check('the PNG, JPEG and GIF of a new folder are indexed', total === base + 3, `${base} -> ${total}`)

  await settings('indexing')
  await click('[data-format="png"]')
  const off = (await lib(page, 'Settings')).excludedFormats
  check('turning PNG off in Settings is saved', JSON.stringify(off) === '["png"]', JSON.stringify(off))
  await page.screenshot({ path: join(s.out, 'formats.png') })

  ffmpeg('-f', 'lavfi', '-i', 'testsrc=size=300x200', '-frames:v', '1', join(extra, 'second.png'))
  await lib(page, 'Rescan')
  await sleep(2500)
  const while_off = (await waitScanned()).total
  check(
    'with PNG off a new PNG is not added and the one in the library stays',
    while_off === total,
    `${total} -> ${while_off}`,
  )

  await click('[data-format="png"]')
  const on = (await waitScanned(total + 1)).total
  check('turning PNG on again finds the file left out', on === total + 1, `${while_off} -> ${on}`)
  check(
    'no format is left off',
    (await lib(page, 'Settings')).excludedFormats === null ||
      (await lib(page, 'Settings')).excludedFormats.length === 0,
  )
})

await guard('animations', async () => {
  if (!total) throw new Error('needs the formats section')
  const days = await lib(page, 'Timeline', { filter: 'all' })
  const [today] = await lib(page, 'Days', days[0].day, days[0].day, { filter: 'all' })
  let gif
  for (const it of today.items) {
    if ((await lib(page, 'Asset', it.id)).ext.toLowerCase() === '.gif') gif = it
  }
  if (!gif) throw new Error('the GIF is not in the newest day')
  await settings('viewing')
  await click('[data-viewing="animations"]')
  check('holding animations is saved', (await prefs()).viewer.stillAnimations === true)
  await go(page, '/', 900)
  await openViewer(gif.id)
  await page.waitForSelector('[data-animation]', { timeout: 15000 })
  await sleep(800)
  const src = () => page.$eval('.image-surface img.original', (img) => img.getAttribute('src'))
  const held = await src()
  check('an animated GIF waits on a still copy of its first frame', held.includes('/media/render/'), held)
  const cache = await media(page, 'ViewingCacheBytes')
  check('that still copy is counted in the viewing cache', cache > 0, `${cache} bytes`)
  await page.click('[data-animation]')
  await sleep(700)
  const playing = await src()
  check('Play animation shows the original, which animates', playing.includes('/media/original/'), playing)
  await page.screenshot({ path: join(s.out, 'animation.png') })
  await closeViewer()

  const thumbs = await media(page, 'ThumbCacheBytes')
  await settings('indexing')
  await click('[data-viewing-cache-clear]')
  await sleep(600)
  check(
    'clearing the viewing copies empties only that cache',
    (await media(page, 'ViewingCacheBytes')) === 0 && (await media(page, 'ThumbCacheBytes')) === thumbs,
    `viewing ${await media(page, 'ViewingCacheBytes')}, thumbnails ${thumbs} -> ${await media(page, 'ThumbCacheBytes')}`,
  )
  check('the photo is still in the library after clearing', (await lib(page, 'Asset', gif.id)).id === gif.id)
})

await guard('presets', async () => {
  await settings('files')
  await page.type('[data-preset-name]', 'Web 2K')
  await click('[data-preset-add]')
  const saved = (await prefs()).exportPresets ?? []
  check(
    'an export preset is saved with a format, size and quality, and no folder',
    saved.length === 1 &&
      saved[0].name === 'Web 2K' &&
      saved[0].format === 'jpeg' &&
      !JSON.stringify(saved).includes('\\'),
    JSON.stringify(saved),
  )
})

await guard('reset', async () => {
  const sources = await lib(page, 'Sources')
  const before = await lib(page, 'Settings')
  const photosBefore = (await lib(page, 'Progress')).total
  await lib(page, 'SetPauseBusy', false)
  await settings('appearance')
  await click('[data-category="appearance"] [data-reset-section]')
  await confirm('Reset')
  const one = await prefs()
  check(
    'resetting Appearance restores it and leaves the other sections',
    one.motion === 'system' &&
      one.viewerBackground === 'dark' &&
      one.focusRing === 'strong' &&
      one.exportPresets?.length === 1,
    JSON.stringify({
      motion: one.motion,
      bg: one.viewerBackground,
      focus: one.focusRing,
      presets: one.exportPresets?.length,
    }),
  )
  check('the page follows the reset at once', (await root('viewerBg')) === 'dark')

  await settings('presets')
  await click('[data-reset-all]')
  await confirm('Reset all settings')
  await sleep(800)
  const all = await prefs()
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b)
  check('Reset all settings restores every preference', same(all, defaults), JSON.stringify(all).slice(0, 300))
  const after = await lib(page, 'Settings')
  check(
    'Reset all settings restores the engine settings it names',
    after.pauseBusy === true && after.idleOnly === false,
    JSON.stringify(after),
  )
  check(
    'a reset leaves videos, text reading, XMP and the folders as they were',
    after.videos === before.videos &&
      after.ocr === before.ocr &&
      after.xmp === before.xmp &&
      same(await lib(page, 'Sources'), sources),
  )
  const photosAfter = (await lib(page, 'Progress')).total
  check('a reset leaves the library’s photos', photosAfter === photosBefore, `${photosBefore} -> ${photosAfter}`)
  await page.screenshot({ path: join(s.out, 'reset.png') })
})

check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors().join(' || '))
check('no request left the PC', external.length === 0, external.join(' '))
await close(app)
s.finish()
