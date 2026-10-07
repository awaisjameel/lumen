// Collections through the real bridge: an album made in the UI, membership
// through a reload, Trash from the viewer and the toast's Undo, a smart album
// following favourites, a custom Thing, and About's version.
//   node suites/collections.mjs
import { join } from 'node:path'
import { close, go, launch, lib, resize, search, sleep, system } from '../lib/app.mjs'
import { libraryCopy } from '../lib/fixtures.mjs'
import { suite, version } from '../lib/suite.mjs'

const s = suite('collections')
const { must } = s
const PHOTO = 250 // an asset the base library leaves alone: no favourite, album or Trash

async function clickText(page, selector, text) {
  const visible = (selector, text) =>
    [...document.querySelectorAll(selector)].find(
      (e) =>
        e.textContent.trim() === text && e.getBoundingClientRect().width > 0 && e.getBoundingClientRect().height > 0,
    )
  await page.waitForFunction(visible, {}, selector, text)
  const handle = await page.evaluateHandle(visible, selector, text)
  const element = handle.asElement()
  if (!element) throw new Error(`Missing control: ${text}`)
  await element.click()
  await handle.dispose()
}

const app = await launch({ exe: s.exe, dataDir: await libraryCopy(join(s.run, 'data')), port: s.port })
const { page } = app
s.watch(page)
try {
  await resize(page, 1440, 900)
  await page.waitForSelector('.scroller')

  await go(page, '/albums', 600)
  await clickText(page, 'button', 'New album')
  await page.waitForSelector('#album-editor input')
  await page.type('#album-editor input', 'End to end album')
  await page.$eval('#album-editor', (form) => form.requestSubmit())
  await page.waitForFunction(() => /^\/albums\/\d+$/.test(location.pathname))
  const albumId = await page.evaluate(() => Number(location.pathname.split('/').at(-1)))
  must(
    'an album created in the UI persists through the real bridge',
    (await lib(page, 'Albums')).some((a) => a.id === albumId && a.name === 'End to end album'),
  )
  await lib(page, 'SetAlbumItem', PHOTO, albumId, true)
  await page.reload()
  await page.waitForSelector('.scroller .cell')
  must(
    'manual album membership survives a reload and renders the photo',
    (await lib(page, 'AssetAlbums', PHOTO)).includes(albumId) && !!(await page.$(`#cell-${PHOTO}`)),
  )

  const before = await lib(page, 'TrashCount')
  await page.focus('.scroller')
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('Enter')
  await page.waitForSelector('[aria-modal="true"] img')
  await page.click('[aria-modal="true"] button[aria-label="Move to Trash"]')
  await page.waitForFunction(() => !document.querySelector('[aria-modal="true"] img'))
  must("the viewer's Trash control moves the album's photo to Trash", (await lib(page, 'TrashCount')) === before + 1)
  await page.waitForFunction(() =>
    [...document.querySelectorAll('button')].some((e) => e.textContent.trim() === 'Undo'),
  )
  await sleep(250)
  await clickText(page, 'button', 'Undo')
  await page.waitForSelector(`#cell-${PHOTO}`)
  must(
    "the toast's Undo restores the photo and its album membership",
    (await lib(page, 'TrashCount')) === before && (await lib(page, 'AssetAlbums', PHOTO)).includes(albumId),
  )
  await lib(page, 'DeleteAlbum', albumId)

  const rule = { kind: 'photo', favourites: true, from: '', to: '', tag: 0, person: 0 }
  const smart = await lib(page, 'CreateAlbum', 'End to end smart album', 'smart', rule, [])
  await lib(page, 'SetFavourite', PHOTO, true)
  const count = (await lib(page, 'Albums')).find((a) => a.id === smart).count
  await lib(page, 'SetFavourite', PHOTO, false)
  must(
    'a smart album follows favourites as they change',
    (await lib(page, 'Albums')).find((a) => a.id === smart).count === count - 1,
    `${count} -> ${count - 1}`,
  )
  await lib(page, 'DeleteAlbum', smart)

  await go(page, '/things', 600)
  await page.waitForSelector('form.new input')
  await page.type('form.new input', 'End to end red circle')
  await page.$eval('form.new', (form) => form.requestSubmit())
  await page.waitForFunction(() => /^\/things\/\d+$/.test(location.pathname), { timeout: 30000 })
  const tag = await page.evaluate(() => Number(location.pathname.split('/').at(-1)))
  must(
    'a custom Thing created in the UI reaches the semantic search engine',
    (await search(page, 'Things')).some((t) => t.id === tag && t.name === 'End to end red circle'),
  )
  await search(page, 'DeleteTag', tag)

  await go(page, '/settings', 600)
  const want = version()
  await page.waitForFunction(
    (want) => document.body.innerText.includes('About Lumen') && document.body.innerText.includes(want),
    {},
    want,
  )
  must('About shows the version the build was given', (await system(page, 'About')).version === want, want)
  await page.screenshot({ path: join(s.out, 'settings.png') })
  s.check('no uncaught frontend errors', s.appErrors().length === 0, s.appErrors().join(' || '))
} catch (error) {
  s.check('the workflows ran to the end', false, String(error))
  console.log(await page.evaluate(() => document.body.innerText.slice(-1200)).catch(() => 'page unavailable'))
  await page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  await close(app)
  s.finish()
}
