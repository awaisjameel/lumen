// Interface acceptance in the native WebView2: themes and preferences,
// overlays and focus, keyboard, Escape, state through refresh and filtered
// removal, reduced motion, forced colours, DPI, narrow windows, video
// playback, and immediate privacy revocation. Each check records pass/fail
// with what was observed. SECTIONS=themes,video runs only those sections.
//   node suites/interface.mjs
import { join } from 'node:path'
import { close, launch, lib, resize, sleep } from '../lib/app.mjs'
import { fixture, libraryCopy } from '../lib/fixtures.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('interface')
const { check, guard, errors } = s
const outDir = s.out
const videosDir = fixture('videos')
const app = await launch({ exe: s.exe, dataDir: await libraryCopy(join(s.run, 'data')), port: s.port })
const page = app.page
const responses = []
page.on('response', (r) => {
  if (r.status() >= 400) responses.push({ status: r.status(), url: r.url() })
})
s.watch(page)
await resize(page, 1440, 900)
await sleep(2500)

const shot = (name) => page.screenshot({ path: join(outDir, `${name}.png`) })
const go = async (path) => {
  await page.evaluate((path) => {
    history.pushState({}, '', path)
    window.dispatchEvent(new PopStateEvent('popstate'))
  }, path)
  await sleep(1000)
}
const gridReady = () =>
  page.waitForFunction(() => document.querySelectorAll('.scroller .cell img').length >= 6, { timeout: 20000 })
const emulation = await page.createCDPSession()
const media = async (features) => {
  await emulation.send('Emulation.setEmulatedMedia', { features })
  await sleep(400)
}
const prefs = () => lib(page, 'Preferences')
const setPrefs = async (patch) => {
  await lib(page, 'SetPreferences', { ...(await prefs()), ...patch })
  await page.reload()
  await sleep(2000)
}
const byText = (selector, text) =>
  page.evaluateHandle(
    (selector, text) =>
      [...document.querySelectorAll(selector)].find((e) => e.textContent.trim().includes(text)) ?? null,
    selector,
    text,
  )

// --- 1. Themes, accent, density, text scale --------------------------------------
await guard('themes', async () => {
  await gridReady()
  const start = await prefs()
  await setPrefs({ theme: 'dark', accent: 'teal', density: 'compact', textScale: 120 })
  await gridReady()
  const dark = await page.evaluate(() => ({
    dark: document.documentElement.classList.contains('dark'),
    accent: document.documentElement.dataset.accent,
    density: document.documentElement.dataset.density,
    rootFont: getComputedStyle(document.documentElement).fontSize,
    bg: getComputedStyle(document.body).backgroundColor,
    scheme: getComputedStyle(document.documentElement).colorScheme,
  }))
  check(
    'dark theme, accent, density and text scale survive a restart of the page',
    dark.dark && dark.accent === 'teal' && dark.density === 'compact' && dark.rootFont === '19.2px',
    JSON.stringify(dark),
  )
  await shot('theme-dark-compact-teal-120')

  // Portalled content follows the theme: the palette's dialog is dark too.
  await page.keyboard.down('Control')
  await page.keyboard.press('k')
  await page.keyboard.up('Control')
  await page.waitForSelector('[role="dialog"]', { timeout: 3000 }).catch(() => {})
  await sleep(300)
  const portal = await page.evaluate(() => {
    const d = document.querySelector('[role="dialog"]')
    return d ? { bg: getComputedStyle(d).backgroundColor, inDark: !!d.closest('.dark') } : null
  })
  check('a portalled overlay takes the dark theme', portal?.inDark, JSON.stringify(portal))
  await shot('theme-dark-palette')
  await page.keyboard.press('Escape')
  await sleep(400)

  await setPrefs({ theme: 'light', accent: 'blue', density: 'comfortable', textScale: 100 })
  await gridReady()
  const light = await page.evaluate(() => ({
    dark: document.documentElement.classList.contains('dark'),
    rootFont: getComputedStyle(document.documentElement).fontSize,
    bg: getComputedStyle(document.body).backgroundColor,
  }))
  check(
    'light theme applies',
    // The window behind the inset sheet is the light base colour.
    !light.dark && light.rootFont === '16px' && light.bg === 'rgb(236, 238, 243)',
    JSON.stringify(light),
  )
  await shot('theme-light')

  await setPrefs({ theme: 'system' })
  await media([{ name: 'prefers-color-scheme', value: 'dark' }])
  const sysDark = await page.evaluate(() => document.documentElement.classList.contains('dark'))
  await media([{ name: 'prefers-color-scheme', value: 'light' }])
  const sysLight = await page.evaluate(() => document.documentElement.classList.contains('dark'))
  check('"system" follows Windows while running', sysDark && !sysLight, `dark=${sysDark} light=${sysLight}`)
  await media([])
  await lib(page, 'SetPreferences', start)
  await page.reload()
  await sleep(2000)
})

// --- 2. Command palette, shortcuts, focus return ---------------------------------
await guard('overlays', async () => {
  await go('/')
  await gridReady()
  await page.focus('.scroller')
  await page.keyboard.down('Control')
  await page.keyboard.press('k')
  await page.keyboard.up('Control')
  await sleep(600)
  const open = await page.evaluate(() => ({
    dialog: !!document.querySelector('[role="dialog"][data-state="open"]'),
    focusInDialog: !!document.activeElement?.closest('[role="dialog"]'),
    focusTag: document.activeElement?.tagName,
  }))
  check(
    'Ctrl+K opens the command palette with focus in its field',
    open.dialog && open.focusInDialog && open.focusTag === 'INPUT',
    JSON.stringify(open),
  )

  // Page shortcuts are suspended under the overlay: F must not favourite.
  await page.keyboard.type('people')
  await sleep(300)
  const listed = await page.evaluate(() => document.querySelector('[role="dialog"]')?.textContent ?? '')
  check('the palette filters commands as you type', /Go to People/.test(listed), listed.slice(0, 120))
  await page.keyboard.press('Enter')
  await sleep(1000)
  check(
    'choosing a command runs it and closes the palette',
    (await page.evaluate(() => location.pathname)) === '/people' &&
      !(await page.$('[role="dialog"][data-state="open"]')),
  )

  await go('/')
  await gridReady()
  await page.focus('.scroller')
  await page.keyboard.down('Control')
  await page.keyboard.press('k')
  await page.keyboard.up('Control')
  await sleep(500)
  await page.keyboard.press('Escape')
  await sleep(500)
  const back = await page.evaluate(() => ({
    closed: !document.querySelector('[role="dialog"][data-state="open"]'),
    focus: document.activeElement?.className ?? '',
  }))
  check(
    'Escape closes the palette and focus returns to the grid',
    back.closed && /scroller/.test(back.focus),
    JSON.stringify(back),
  )

  await page.keyboard.press('?')
  await sleep(500)
  const help = await page.evaluate(() => document.querySelector('[role="dialog"]')?.textContent ?? '')
  check(
    '? opens the shortcut reference from the registry',
    /Keyboard shortcuts/.test(help) && /Ctrl/.test(help),
    help.slice(0, 100),
  )
  await shot('shortcut-reference')
  await page.keyboard.press('Escape')
  await sleep(400)

  // Tooltip on a toolbar control.
  const info = await page.$('button[aria-label="Details"]')
  await info?.hover()
  await sleep(900)
  const tip = await page.evaluate(() => document.querySelector('[role="tooltip"]')?.textContent ?? '')
  check('a toolbar tooltip shows the name and its key', /details\s*I$/i.test(tip.trim()), tip)
  await shot('tooltip')
  await page.mouse.move(700, 500)
})

// --- 3. Keyboard: grid, viewer, focus ----------------------------------------------
let firstId = 0
await guard('keyboard', async () => {
  await go('/')
  await gridReady()
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await sleep(300)
  const one = await page.evaluate(() => document.querySelector('.scroller [aria-selected="true"]')?.id ?? '')
  await page.keyboard.press('ArrowRight')
  await sleep(300)
  const two = await page.evaluate(() => document.querySelector('.scroller [aria-selected="true"]')?.id ?? '')
  check('arrow keys select and move in the grid', one && two && one !== two, `${one} -> ${two}`)
  firstId = Number(two.replace('cell-', ''))
  const active = await page.evaluate(() => document.querySelector('.scroller')?.getAttribute('aria-activedescendant'))
  check('the grid names the selected cell as its active descendant', active === two, String(active))

  await page.keyboard.press('i')
  await sleep(700)
  const inspector = await page.evaluate(() => !!document.querySelector('aside'))
  check('I shows the details inspector', inspector)
  await shot('inspector-wide')
  await page.focus('.scroller')
  await page.keyboard.press('i')
  await sleep(400)

  await page.keyboard.press('Enter')
  await page.waitForFunction(() => !!document.querySelector('[aria-modal="true"] img, [role="dialog"] img'), {
    timeout: 10000,
  })
  await sleep(600)
  await shot('viewer')
  const src1 = await page.evaluate(() =>
    [...document.querySelectorAll('[aria-modal="true"] img, [role="dialog"] img')].map((i) => i.currentSrc).join('|'),
  )
  await page.keyboard.press('ArrowRight')
  await sleep(900)
  const src2 = await page.evaluate(() =>
    [...document.querySelectorAll('[aria-modal="true"] img, [role="dialog"] img')].map((i) => i.currentSrc).join('|'),
  )
  check('→ in the viewer moves to the next photo', src1 !== src2, `${src1.slice(-30)} -> ${src2.slice(-30)}`)

  // Page-wide shortcuts wait while the viewer covers the page.
  await page.keyboard.down('Control')
  await page.keyboard.press('k')
  await page.keyboard.up('Control')
  await sleep(500)
  check('Ctrl+K does not open the palette over the viewer', !(await page.$('[role="dialog"][data-state="open"] input')))

  await page.keyboard.press('Escape')
  await sleep(700)
  const after = await page.evaluate(() => ({
    viewer: !!document.querySelector('[aria-modal="true"] img'),
    focus: document.activeElement?.className ?? '',
    selected: document.querySelector('.scroller [aria-selected="true"]')?.id ?? '',
    active: document.querySelector('.scroller')?.getAttribute('aria-activedescendant') ?? '',
  }))
  check(
    'Escape closes the viewer; focus returns to the grid, on the photo last viewed',
    !after.viewer && /scroller/.test(after.focus) && after.active && after.active !== two,
    JSON.stringify(after),
  )
  // The cell shown as selected is the one the grid's keys act on next, so it
  // must be the photo last viewed too, not the one the viewer was opened on.
  check(
    'the selection shown in the grid follows to the photo last viewed',
    after.selected && after.selected === after.active,
    JSON.stringify(after),
  )
})

// --- 3b. Escape ownership: a confirmation above the viewer -----------------------
await guard('escape', async () => {
  const trashBefore = await lib(page, 'TrashCount')
  await go('/trash')
  await sleep(1500)
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('Enter')
  await page.waitForFunction(() => !!document.querySelector('[aria-modal="true"] img'), { timeout: 10000 })
  await sleep(500)
  const del = await byText('[aria-modal="true"] button', 'Delete from disk')
  await del.asElement()?.click()
  await sleep(900)
  const asked = await page.evaluate(() => {
    const d = [...document.querySelectorAll('[role="dialog"][data-state="open"], dialog[open]')].find((x) =>
      /Recycle Bin|file/.test(x.textContent),
    )
    return d
      ? {
          text: d.textContent.trim().slice(0, 90),
          focusInside: d.contains(document.activeElement),
          focus: document.activeElement?.textContent?.trim(),
        }
      : null
  })
  check(
    'Delete from disk asks first, with Cancel focused',
    !!asked && asked.focusInside && /Cancel/.test(asked.focus ?? ''),
    JSON.stringify(asked),
  )
  await shot('viewer-confirm')
  await page.keyboard.press('Escape')
  await sleep(700)
  const after = await page.evaluate(() => ({
    confirm: !![...document.querySelectorAll('[role="dialog"][data-state="open"], dialog[open]')].find((x) =>
      /Recycle Bin/.test(x.textContent),
    ),
    viewer: !!document.querySelector('[aria-modal="true"] img'),
  }))
  check(
    'Escape closes only the confirmation; the viewer stays open and nothing is deleted',
    !after.confirm && after.viewer && (await lib(page, 'TrashCount')) === trashBefore,
    JSON.stringify(after),
  )
  await page.keyboard.press('Escape')
  await sleep(600)
  check('a second Escape closes the viewer', !(await page.$('[aria-modal="true"] img')))
})

// --- 4. State through refresh and filtered removal ---------------------------------
await guard('filtered removal', async () => {
  await go('/favourites')
  await gridReady().catch(() => undefined)
  const before = await page.evaluate(() => document.querySelectorAll('.scroller .cell[role="option"]').length)
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await sleep(300)
  const sel = await page.evaluate(() => document.querySelector('.scroller [aria-selected="true"]')?.id ?? '')
  await page.keyboard.press('f') // unfavourite: the photo leaves this view
  await sleep(1500)
  const after = await page.evaluate(
    (sel) => ({
      count: document.querySelectorAll('.scroller .cell[role="option"]').length,
      stillThere: !!document.getElementById(sel),
      selected: document.querySelector('.scroller [aria-selected="true"]')?.id ?? '',
      active: document.querySelector('.scroller')?.getAttribute('aria-activedescendant') ?? '',
    }),
    sel,
  )
  check(
    'unfavouriting in Favourites removes the photo and never leaves the selection on a missing cell',
    after.count === before - 1 && !after.stillThere && (after.selected === '' || !!after.active),
    JSON.stringify({ before, sel, ...after }),
  )
  // Put it back, as the seed had it.
  await lib(page, 'SetFavourite', Number(sel.replace('cell-', '')), true)

  // In the viewer: unfavourite while viewing a favourite moves on instead of showing a stale photo.
  await go('/favourites')
  await sleep(1500)
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('Enter')
  await page.waitForFunction(() => !!document.querySelector('[aria-modal="true"] img'), { timeout: 10000 })
  await sleep(500)
  const v1 = await page.evaluate(() => document.querySelector('[aria-modal="true"] img')?.currentSrc ?? '')
  const id1 = Number((v1.match(/\/(\d+)\//) ?? [])[1] ?? 0)
  await page.keyboard.press('f')
  await sleep(1500)
  const v2 = await page.evaluate(() => ({
    open: !!document.querySelector('[aria-modal="true"]'),
    src: document.querySelector('[aria-modal="true"] img')?.currentSrc ?? '',
  }))
  check(
    'unfavouriting in the viewer moves to another photo (or closes), never keeps the removed one',
    !v2.open || (v2.src && v2.src !== v1),
    JSON.stringify({ v1: v1.slice(-28), ...v2, src: v2.src.slice(-28) }),
  )
  if (id1) await lib(page, 'SetFavourite', id1, true)
  await page.keyboard.press('Escape')
  await sleep(500)

  // A live library change while a photo is selected keeps the selection.
  await go('/')
  await gridReady()
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await sleep(300)
  const kept = await page.evaluate(() => document.querySelector('.scroller [aria-selected="true"]')?.id ?? '')
  await lib(page, 'Rescan')
  await sleep(5000)
  const still = await page.evaluate(() => document.querySelector('.scroller [aria-selected="true"]')?.id ?? '')
  check('a rescan (live library refresh) keeps the selection', kept && kept === still, `${kept} / ${still}`)
})

// --- 5. Reduced motion, forced colours, DPI -----------------------------------------
await guard('media features', async () => {
  await go('/search?q=zzzz-nothing-like-this')
  await media([{ name: 'prefers-reduced-motion', value: 'reduce' }])
  await go('/')
  await gridReady()
  const motion = await page.evaluate(() => {
    const probe = document.createElement('div')
    probe.style.animation = 'lumen-spin 1s linear infinite'
    probe.style.transition = 'opacity 1s'
    document.body.append(probe)
    const cs = getComputedStyle(probe)
    const out = {
      animation: cs.animationDuration,
      iterations: cs.animationIterationCount,
      transition: cs.transitionDuration,
    }
    probe.remove()
    return out
  })
  check(
    'reduced motion stops animation and transitions',
    parseFloat(motion.animation) < 0.001 && motion.iterations === '1' && parseFloat(motion.transition) < 0.001,
    JSON.stringify(motion),
  )
  await media([])

  await media([{ name: 'forced-colors', value: 'active' }])
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await sleep(400)
  const forced = await page.evaluate(() => {
    const matches = matchMedia('(forced-colors: active)').matches
    const btn = document.querySelector('button[aria-label="Details"]')
    btn?.focus()
    const cs = btn ? getComputedStyle(btn) : null
    return { matches, outlineStyle: cs?.outlineStyle, outlineWidth: cs?.outlineWidth }
  })
  check(
    'forced colours: focus is an outline, not a shadow',
    forced.matches && forced.outlineStyle === 'solid' && parseFloat(forced.outlineWidth) >= 1.5,
    JSON.stringify(forced),
  )
  await shot('forced-colours')
  await go('/settings')
  await sleep(600)
  await shot('forced-colours-settings')
  await media([])

  for (const scale of [1.25, 1.5, 2]) {
    const s = emulation
    await s.send('Emulation.setDeviceMetricsOverride', {
      width: Math.round(1440 / scale),
      height: Math.round(900 / scale),
      deviceScaleFactor: scale,
      mobile: false,
    })
    await go('/')
    await sleep(1200)
    const m = await page.evaluate(() => ({
      dpr: devicePixelRatio,
      w: innerWidth,
      overflow: document.documentElement.scrollWidth - innerWidth,
      cells: document.querySelectorAll('.scroller .cell').length,
    }))
    await shot(`dpi-${scale * 100}`)
    await go('/settings')
    await sleep(700)
    const st = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)
    await shot(`dpi-${scale * 100}-settings`)
    check(
      `${scale * 100}% DPI: grid renders and nothing overflows sideways`,
      Math.abs(m.dpr - scale) < 0.01 && m.overflow <= 0 && st <= 0 && m.cells > 0,
      JSON.stringify({ ...m, settingsOverflow: st }),
    )
    await s.send('Emulation.clearDeviceMetricsOverride')
  }
})

// --- 6. Narrow window: drawer, overflow, inspector sheet ------------------------------
await guard('narrow', async () => {
  await resize(page, 700, 760)
  await go('/')
  await gridReady()
  const narrow = await page.evaluate(() => ({
    sidebar: !!document.querySelector('aside.sidebar'),
    menu: !!document.querySelector('button[aria-label="Open navigation"]'),
    overflow: document.documentElement.scrollWidth - innerWidth,
  }))
  check(
    'narrow window: no sidebar, a navigation button, no sideways overflow',
    !narrow.sidebar && narrow.menu && narrow.overflow <= 0,
    JSON.stringify(narrow),
  )
  await page.click('button[aria-label="Open navigation"]')
  await sleep(700)
  const drawer = await page.evaluate(() => ({
    open: !!document.querySelector('[role="dialog"][data-state="open"]'),
    links: [...document.querySelectorAll('[role="dialog"] a')].map((a) => a.textContent.trim()).filter(Boolean),
    focusInside: !!document.activeElement?.closest('[role="dialog"]'),
  }))
  check(
    'the navigation drawer opens with every destination and holds the focus',
    // A link's text ends with its count ("People0").
    drawer.open &&
      drawer.links.some((l) => l.startsWith('People')) &&
      drawer.links.includes('Settings') &&
      drawer.focusInside,
    JSON.stringify(drawer),
  )
  await shot('narrow-drawer')
  await page.keyboard.press('Escape')
  await sleep(600)
  const closed = await page.evaluate(() => ({
    open: !!document.querySelector('[role="dialog"][data-state="open"]'),
    focus: document.activeElement?.getAttribute('aria-label') ?? '',
  }))
  check(
    'Escape closes the drawer and focus returns to its button',
    !closed.open && closed.focus === 'Open navigation',
    JSON.stringify(closed),
  )

  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('i')
  await sleep(900)
  const sheet = await page.evaluate(() => !!document.querySelector('[role="dialog"][data-state="open"]'))
  check('narrow window: the inspector opens as a sheet', sheet)
  await shot('narrow-inspector-sheet')
  await page.keyboard.press('Escape')
  await sleep(600)
  await resize(page, 1000, 800)
  await go('/')
  await sleep(800)
  const medium = await page.evaluate(() => ({
    rail: !!document.querySelector('aside.sidebar.collapsed'),
    overflow: document.documentElement.scrollWidth - innerWidth,
  }))
  check('medium window: the sidebar is an icon rail', medium.rail && medium.overflow <= 0, JSON.stringify(medium))
  await shot('medium-rail')
  await resize(page, 1440, 900)
})

// --- 7. Video playback: direct and converted ------------------------------------------
await guard('video', async () => {
  const sources = await lib(page, 'Sources')
  if (!sources.some((s) => s.path.toLowerCase().includes('videos'))) {
    await lib(page, 'SetVideos', true)
    await lib(page, 'AddSources', [videosDir])
  }
  for (let i = 0; i < 120; i++) {
    const p = await lib(page, 'Progress')
    if (!p.scanning && p.metadata + p.thumbnail === 0) break
    await sleep(1000)
  }
  await page.reload()
  await sleep(2000)
  await go('/videos')
  await sleep(2500)
  const n = await page.evaluate(() => document.querySelectorAll('.scroller .cell[role="option"]').length)
  check('the Videos view lists the test videos', n >= 3, `cells=${n}`)
  await shot('videos')
  const seen = []
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('Enter')
  for (let i = 0; i < n && i < 3; i++) {
    try {
      await page.waitForFunction(
        () => {
          const v = document.querySelector('[aria-modal="true"] video')
          return v && v.readyState >= 2
        },
        { timeout: 30000 },
      )
      await page.evaluate(() => {
        const v = document.querySelector('[aria-modal="true"] video')
        v.muted = true
        return v.play()
      })
      await sleep(2500)
      const v = await page.evaluate(() => {
        const v = document.querySelector('[aria-modal="true"] video')
        return {
          src: v.currentSrc.replace(/^https?:\/\/[^/]+/, ''),
          t: v.currentTime,
          w: v.videoWidth,
          err: v.error?.message ?? '',
          paused: v.paused,
        }
      })
      seen.push(v)
    } catch (e) {
      seen.push({ err: String(e).slice(0, 150) })
    }
    await shot(`video-${i}`)
    await page.keyboard.press('ArrowRight')
    await sleep(1200)
  }
  const direct = seen.filter((v) => v.src?.includes('/video/') && v.t > 0.5 && v.w > 0)
  const converted = seen.filter((v) => v.src?.includes('/play/') && v.t > 0.5 && v.w > 0)
  check('a video the WebView plays directly plays', direct.length >= 1, JSON.stringify(seen))
  check('a video that needs conversion plays through the converter', converted.length >= 1, JSON.stringify(seen))
  await page.keyboard.press('Escape')
  await sleep(600)
})

// --- 8. Privacy: lock revokes covered media at once ---------------------------------
await guard('privacy', async () => {
  const PIN = '246810'
  if (!(await lib(page, 'BlockPINConfigured'))) await lib(page, 'CreateBlockPIN', PIN)
  await lib(page, 'UnlockBlocks', PIN)
  const victim = 200
  let blocks = await lib(page, 'Blocks')
  if (!blocks?.length) {
    await lib(page, 'SetBlock', 'asset', victim, '', 'photos')
    blocks = await lib(page, 'Blocks')
  }
  check('a block exists for the test', blocks.length >= 1, JSON.stringify(blocks).slice(0, 200))

  // The blocked photo is gone from the library and its media is refused.
  const hidden = await page.evaluate(async (id) => {
    const r = await fetch(`/media/thumb/${id}/256?v=1`)
    return r.status
  }, victim)
  await lib(page, 'LockBlocks')
  const locked = await page.evaluate(async (id) => (await fetch(`/media/thumb/${id}/256?v=1`)).status, victim)
  check("a blocked photo's media is not served while locked", locked >= 400, `unlocked=${hidden} locked=${locked}`)

  await go('/settings/blocked')
  await sleep(1200)
  await shot('blocked-locked')
  const pin = await page.$('input[type="password"], input[inputmode="numeric"]')
  check('the Blocked screen starts locked, asking for the PIN', !!pin)
  await pin?.type(PIN)
  await page.keyboard.press('Enter')
  await sleep(1500)
  await shot('blocked-unlocked')
  const unlockedText = await page.evaluate(() => document.querySelector('main')?.textContent ?? '')
  check(
    'the PIN unlocks the block list',
    /Unblock|Switch mode|Photo or video/.test(unlockedText),
    unlockedText.slice(0, 160),
  )

  // Open the covered media of the block.
  const browse = await byText('main button', 'Show')
  const el = browse.asElement() ?? (await byText('main button, main a', 'covered')).asElement()
  if (el) {
    await el.click()
    await sleep(1500)
  }
  const covered = () => page.evaluate(() => document.querySelectorAll('main img[src*="/media/"]').length)
  const coveredBefore = await covered()
  await shot('blocked-covered')
  check(
    'the covered-media browser shows the blocked photo behind the PIN',
    coveredBefore >= 1,
    `images=${coveredBefore}`,
  )

  // Lock with the screen's own control: the media must be gone in the same moment.
  const lock = (await byText('main button, header button', 'Lock')).asElement()
  await lock?.click()
  const atOnce = await page.evaluate(() => ({
    images: document.querySelectorAll('main img[src*="/media/"]').length,
    pin: !!document.querySelector('input[type="password"], input[inputmode="numeric"]'),
  }))
  await sleep(600)
  const fetchedAfterLock = await page.evaluate(async (id) => (await fetch(`/media/thumb/${id}/256?v=2`)).status, victim)
  await shot('blocked-relocked')
  check(
    'locking removes covered media from the screen at once and asks for the PIN again',
    !!lock && atOnce.images === 0 && atOnce.pin,
    JSON.stringify(atOnce),
  )
  check('covered media cannot be fetched after the lock', fetchedAfterLock >= 400, `status=${fetchedAfterLock}`)

  // Leaving the screen locks too.
  const pin2 = await page.$('input[type="password"], input[inputmode="numeric"]')
  await pin2?.type(PIN)
  await page.keyboard.press('Enter')
  await sleep(1500)
  const openStatus = await page.evaluate(async (id) => (await fetch(`/media/thumb/${id}/256?v=3`)).status, victim)
  await go('/')
  await sleep(1200)
  const leftStatus = await page.evaluate(async (id) => (await fetch(`/media/thumb/${id}/256?v=4`)).status, victim)
  check(
    'leaving the Blocked screen locks the engine',
    openStatus === 200 && leftStatus >= 400,
    `unlocked=${openStatus} afterLeaving=${leftStatus}`,
  )

  // Leave the library as it was: unblock.
  await lib(page, 'UnlockBlocks', PIN)
  for (const b of (await lib(page, 'Blocks')) ?? []) await lib(page, 'RemoveBlock', b.id)
  await lib(page, 'LockBlocks')
})

await close(app)
for (const e of errors.slice(0, 15)) console.log(`  error: ${e}`)
s.finish({ responses })
