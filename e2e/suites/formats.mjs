// The formats beyond JPEG, in the native WebView2: camera RAW of every family,
// JPEG XL, animated PNG, multi-page TIFF and the older video containers, from
// indexing to the viewer. Each check records pass/fail with what was observed.
// The RAW files are real camera samples, downloaded once (fixtures/fetch_raw.py).
//   node suites/formats.mjs
import { basename, join } from 'node:path'
import { close, launch, lib, media as mediaService, resize, sleep } from '../lib/app.mjs'
import { fixture } from '../lib/fixtures.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('formats')
const { check, guard } = s
const outDir = s.out
const corpus = fixture('formats')
const media = (page, method, ...args) => mediaService(page, method, ...args)

const app = await launch({ exe: s.exe, dataDir: join(s.run, 'data'), port: s.port })
const page = app.page
const external = []
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
await sleep(2000)
const shot = (name) => page.screenshot({ path: join(outDir, `${name}.png`) })

// --- the library: one source, everything indexed -------------------------
const sources = await lib(page, 'Sources')
await lib(page, 'SetVideos', true) // off by default
if (!sources.length) await lib(page, 'AddSources', [corpus])
await sleep(3000)
let progress
const started = Date.now()
for (;;) {
  progress = await lib(page, 'Progress')
  if (!progress.scanning && progress.total >= 24 && progress.metadata === 0 && progress.thumbnail === 0) break
  if (Date.now() - started > 600_000) throw new Error(`indexing did not finish: ${JSON.stringify(progress)}`)
  await sleep(1000)
}
const indexSeconds = Math.round((Date.now() - started) / 1000)
await page.reload()
await sleep(2500)

const days = await lib(page, 'Days', '0001-01-01', '9999-12-31', { filter: '' })
const items = days.flatMap((d) => d.items)
const assets = []
for (const it of items) {
  const d = await lib(page, 'Asset', it.id)
  assets.push({ ...d, thumb: it.t, name: basename(d.paths[0]) })
}
const byName = (pattern) => assets.filter((a) => pattern.test(a.name))
const RAW = /\.(dng|cr2|cr3|crw|nef|nrw|arw|sr2|srf|raf|orf|rw2|pef)$/i
console.log(`indexed ${assets.length} assets in ${indexSeconds}s: ${assets.map((a) => a.name).join(', ')}`)

await guard('index', async () => {
  check(
    'TypeScript sources named .ts are not taken for videos',
    !assets.some((a) => /main\.ts$/.test(a.name)),
    assets.map((a) => a.name).join(' '),
  )
  check(
    'a file LibRaw cannot read is skipped, not failed',
    progress.skipped >= 1 && progress.errors === 0,
    JSON.stringify({ skipped: progress.skipped, errors: progress.errors }),
  )
  const lossy = byName(/lossy/i)[0]
  check(
    'the lossy-JPEG DNG stays in the library without a preview',
    lossy && !lossy.thumb && !lossy.caps.preview,
    JSON.stringify(lossy?.caps),
  )
})

// --- camera RAW ----------------------------------------------------------
const raws = byName(RAW).filter((a) => !/lossy|cut/i.test(a.name))
await guard('raw', async () => {
  check('camera RAW files of every family are in the library', raws.length >= 9, raws.map((a) => a.ext).join(' '))
  for (const a of raws) {
    const thumb = await page.evaluate(async (url) => {
      const r = await fetch(url)
      const bitmap = r.ok ? await createImageBitmap(await r.blob()) : null
      return { status: r.status, type: r.headers.get('content-type'), w: bitmap?.width ?? 0, h: bitmap?.height ?? 0 }
    }, `/media/thumb/${a.id}/1024?v=${a.thumb}`)
    const t0 = Date.now()
    const info = await media(page, 'RenderPage', a.id, 4096, 0)
    const ms = Date.now() - t0
    const render = await page.evaluate(
      async (url) => {
        const r = await fetch(url)
        const bitmap = r.ok ? await createImageBitmap(await r.blob()) : null
        return { status: r.status, w: bitmap?.width ?? 0, h: bitmap?.height ?? 0 }
      },
      `/media/render/${a.id}/4096?v=${encodeURIComponent(info.revision)}`,
    )
    const original = await page.evaluate(async (url) => (await fetch(url)).status, `/media/original/${a.id}`)
    const shape = (w, h) => (w / h).toFixed(2)
    const ok =
      a.width > 600 &&
      a.height > 600 &&
      !!a.camera &&
      a.caps.preview &&
      a.caps.render &&
      !a.caps.original &&
      original === 404 &&
      thumb.status === 200 &&
      thumb.type === 'image/jpeg' &&
      Math.max(thumb.w, thumb.h) === 1024 &&
      shape(thumb.w, thumb.h) === shape(a.width, a.height) &&
      render.status === 200 &&
      render.w === info.width &&
      render.h === info.height &&
      !info.preview &&
      Math.max(info.width, info.height) === Math.min(4096, Math.max(a.width, a.height)) &&
      shape(info.width, info.height) === shape(a.width, a.height)
    check(
      `RAW ${a.name}: metadata, thumbnail and developed render agree`,
      ok,
      JSON.stringify({
        size: `${a.width}x${a.height}`,
        camera: a.camera,
        thumb,
        info: `${info.width}x${info.height}`,
        limited: info.limited,
        render,
        original,
        ms,
      }),
    )
  }
  const inspected = await media(page, 'Inspect', raws.find((a) => /\.arw$/i.test(a.name))?.id ?? raws[0].id)
  check('a RAW file reports its exposure', !!inspected.exposure && !!inspected.iso, JSON.stringify(inspected))
})

// --- the other formats, by the engine -------------------------------------
await guard('formats', async () => {
  const jxl = byName(/\.jxl$/i)[0]
  const info = await media(page, 'RenderPage', jxl.id, 4096, 0)
  check(
    'a JPEG XL is indexed and rendered',
    jxl.thumb > 0 && jxl.width === 300 && jxl.height === 200 && info.width === 300 && !jxl.caps.original,
    JSON.stringify({ jxl: jxl.caps, info }),
  )
  const apng = byName(/\.apng$/i)[0]
  const type = await page.evaluate(
    async (url) => (await fetch(url)).headers.get('content-type'),
    `/media/original/${apng.id}`,
  )
  check(
    'an animated PNG is sent to the WebView as it is',
    apng.caps.original && type === 'image/apng' && apng.thumb > 0,
    JSON.stringify({ caps: apng.caps, type }),
  )
  const tiff = byName(/\.tiff?$/i)[0]
  const inspection = await media(page, 'Inspect', tiff.id)
  const colours = []
  for (let p = 0; p < 3; p++) {
    const i = await media(page, 'RenderPage', tiff.id, 4096, p)
    colours.push(
      await page.evaluate(
        async (url) => {
          const r = await fetch(url)
          if (!r.ok) return `status ${r.status}`
          const bitmap = await createImageBitmap(await r.blob())
          const c = new OffscreenCanvas(bitmap.width, bitmap.height).getContext('2d')
          c.drawImage(bitmap, 0, 0)
          const [red, green, blue] = c.getImageData(bitmap.width >> 1, bitmap.height >> 1, 1, 1).data
          return red > 150 ? 'red' : green > 150 ? 'green' : blue > 150 ? 'blue' : `${red},${green},${blue}`
        },
        `/media/render/${tiff.id}/4096?v=${encodeURIComponent(i.revision)}&page=${p}`,
      ),
    )
  }
  check(
    'each page of a TIFF renders as its own picture',
    tiff.caps.paged && inspection.pages === 3 && colours.join() === 'red,green,blue',
    JSON.stringify({ pages: inspection.pages, colours }),
  )
  let refused = ''
  try {
    await media(page, 'RenderPage', tiff.id, 4096, 3)
  } catch (e) {
    refused = String(e).slice(0, 120)
  }
  check('a page the file does not hold is refused', !!refused, refused)
  for (const v of byName(/\.(mpg|mpeg|ts|ogv|asf)$/i))
    check(
      `video ${v.name} is indexed for conversion`,
      v.thumb > 0 && v.video?.play === 'convert' && v.video.duration > 3000 && v.caps.convertPlay && !v.caps.directPlay,
      JSON.stringify(v.video),
    )
  check(
    'all five new video containers were found',
    byName(/\.(mpg|mpeg|ts|ogv|asf)$/i).length === 5,
    byName(/\.(mpg|mpeg|ts|ogv|asf)$/i)
      .map((v) => v.name)
      .join(' '),
  )
})

// --- the viewer: every asset, as the user steps through them -------------
await guard('viewer', async () => {
  await page.waitForFunction(() => document.querySelectorAll('.scroller .cell img').length >= 6, { timeout: 30000 })
  await shot('grid')
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('Enter')
  const seen = new Map()
  for (let i = 0; i < assets.length + 4; i++) {
    await sleep(700)
    const id = await page.evaluate(() => {
      const src =
        document.querySelector('[aria-modal="true"] img.preview')?.src ??
        document.querySelector('[aria-modal="true"] video')?.poster ??
        ''
      return Number(/\/thumb\/(\d+)\//.exec(src)?.[1] ?? 0)
    })
    const a = assets.find((x) => x.id === id)
    if (!a || seen.has(id)) {
      await page.keyboard.press('ArrowRight')
      continue
    }
    let state
    if (a.video) {
      try {
        await page.waitForFunction(() => (document.querySelector('[aria-modal="true"] video')?.readyState ?? 0) >= 2, {
          timeout: 45000,
        })
        await page.evaluate(() => {
          const v = document.querySelector('[aria-modal="true"] video')
          v.muted = true
          return v.play()
        })
        await sleep(2500)
      } catch {
        /* reported below */
      }
      state = await page.evaluate(() => {
        const v = document.querySelector('[aria-modal="true"] video')
        return v
          ? {
              src: v.currentSrc.replace(/^https?:\/\/[^/]+/, '').slice(0, 40),
              t: v.currentTime,
              w: v.videoWidth,
              err: v.error?.message ?? '',
            }
          : { err: 'no video element' }
      })
      if (RegExp(/\.(mpg|mpeg|ts|ogv|asf)$/i).test(a.name))
        check(
          `video ${a.name} plays through the converter`,
          state.t > 0.5 && state.w > 0 && state.src.includes('/play/'),
          JSON.stringify(state),
        )
    } else {
      try {
        await page.waitForFunction(
          () => {
            const img = document.querySelector('[aria-modal="true"] img.original')
            return (
              // The resolution note can show before the developed copy has
              // loaded, so wait for the image itself, or for its failure.
              (img && img.complete && img.naturalWidth > 0 && getComputedStyle(img).display !== 'none') ||
              !!document.querySelector('[aria-modal="true"] .image-surface .failure')
            )
          },
          { timeout: 60000 },
        )
      } catch {
        /* reported below */
      }
      state = await page.evaluate(() => {
        const root = document.querySelector('[aria-modal="true"]')
        const img = root?.querySelector('img.original')
        return {
          src: img?.src.replace(/^https?:\/\/[^/]+/, '').slice(0, 60) ?? '',
          w: img?.naturalWidth ?? 0,
          h: img?.naturalHeight ?? 0,
          shown: !!img && getComputedStyle(img).display !== 'none',
          status: [...(root?.querySelectorAll('.image-surface [role="status"]') ?? [])]
            .map((s) => s.textContent.trim())
            .join(' | '),
          pages: root?.querySelector('.image-controls [data-pages]')?.textContent.trim() ?? '',
        }
      })
      if (RAW.test(a.name) && !/lossy|cut/i.test(a.name))
        check(
          `viewer shows the developed RAW ${a.name}`,
          (state.shown &&
            state.src.includes(`/render/${id}/`) &&
            state.w >= Math.min(4096, Math.max(a.width, a.height)) * 0.99 - 1) ||
            (state.shown && Math.max(state.w, state.h) === Math.min(4096, Math.max(a.width, a.height))),
          JSON.stringify(state),
        )
      if (/cut/i.test(a.name)) {
        check(
          'a RAW that cannot be developed shows its camera preview, and says so',
          state.shown && /Camera preview from the RAW file/.test(state.status),
          JSON.stringify(state),
        )
        await shot('viewer-raw-preview')
      }
      if (/\.jxl$/i.test(a.name))
        check('viewer shows the JPEG XL', state.shown && state.w === 300, JSON.stringify(state))
      if (/\.apng$/i.test(a.name))
        check(
          'viewer shows the animated PNG from its original',
          state.shown && state.src.includes('/original/'),
          JSON.stringify(state),
        )
      if (/\.arw$/i.test(a.name)) await shot('viewer-raw-arw')
      if (/\.raf$/i.test(a.name)) await shot('viewer-raw-raf')
      if (/\.tiff?$/i.test(a.name)) {
        check('viewer offers the pages of a TIFF', /Page 1 of 3/.test(state.pages), JSON.stringify(state))
        await page.click('[aria-modal="true"] .image-controls button[aria-label="Next page"]')
        await page
          .waitForFunction(() => /page=1/.test(document.querySelector('[aria-modal="true"] img.original')?.src ?? ''), {
            timeout: 20000,
          })
          .catch(() => {})
        await sleep(800)
        const turned = await page.evaluate(() => {
          const root = document.querySelector('[aria-modal="true"]')
          const img = root.querySelector('img.original')
          const c = new OffscreenCanvas(img.naturalWidth || 1, img.naturalHeight || 1).getContext('2d')
          c.drawImage(img, 0, 0)
          const [r, g, b] = c.getImageData(img.naturalWidth >> 1, img.naturalHeight >> 1, 1, 1).data
          return {
            pages: root.querySelector('.image-controls [data-pages]')?.textContent.trim(),
            src: img.src.replace(/^https?:\/\/[^/]+/, ''),
            rgb: [r, g, b],
          }
        })
        check(
          'turning the page shows the second page',
          /Page 2 of 3/.test(turned.pages) && /page=1/.test(turned.src) && turned.rgb[1] > 150 && turned.rgb[0] < 80,
          JSON.stringify(turned),
        )
        await shot('viewer-tiff-page2')
      }
    }
    seen.set(id, state)
    await page.keyboard.press('ArrowRight')
  }
  const shown = assets.filter((a) => a.thumb).length
  check('the viewer stepped through every asset that has a preview', seen.size === shown, `${seen.size} of ${shown}`)
  await page.keyboard.press('Escape')
})

await guard('duplicates', async () => {
  const groups = await lib(page, 'DuplicateGroups')
  const mixed = groups.filter((g) => g.kind === 'similar' && new Set(g.items.map((i) => RAW.test(i.ext))).size > 1)
  check(
    'no RAW is offered as similar to a picture of another format',
    mixed.length === 0,
    JSON.stringify(groups.map((g) => g.items.map((i) => i.ext))),
  )
})

check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors().join(' || '))
check('no external requests', external.length === 0, external.join(' '))
await close(app)
s.finish({
  corpus,
  indexSeconds,
  assets: assets.map((a) => ({ name: a.name, width: a.width, height: a.height, camera: a.camera, caps: a.caps })),
})
