// The File tools dialog: its operation menu must open above the dialog and
// take the pointer at every window width, answer the keyboard, and work
// inside a full-screen video too (the frame export).
//   node suites/file-tools.mjs
import { join } from 'node:path'
import { close, launch, lib, resize, sleep } from '../lib/app.mjs'
import { fixture } from '../lib/fixtures.mjs'
import { suite } from '../lib/suite.mjs'

const s = suite('file-tools')
const check = s.must
const source = fixture('media') // only read: nothing is exported or moved
const app = await launch({ exe: s.exe, dataDir: join(s.run, 'data'), port: s.port })
const page = app.page
s.watch(page)
const choose = async (text) => {
  const option = await page.evaluateHandle(
    (text) => [...document.querySelectorAll('[role="option"]')].find((e) => e.textContent.trim() === text),
    text,
  )
  if (!option.asElement()) throw new Error(`Missing ${text}`)
  await option.asElement().click()
  await sleep(150)
}
const listVisible = async (name, count) => {
  await page.waitForSelector('[role="listbox"][data-state="open"]')
  await sleep(250)
  const result = await page.evaluate(() => {
    const list = document.querySelector('[role="listbox"][data-state="open"]')
    const wrapper = list.closest('[data-reka-popper-content-wrapper]')
    const dialog = document.querySelector('[role="dialog"][data-state="open"]')
    const options = [...list.querySelectorAll('[role="option"]')]
    return {
      z: Number(wrapper.style.zIndex),
      dialogZ: Number(dialog.style.zIndex),
      hit: options.every((option) => {
        const r = option.getBoundingClientRect()
        return option.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2))
      }),
      options: options.map((e) => e.textContent.trim()),
    }
  })
  check(name, result.hit && result.z > result.dialogZ && result.options.length === count, result)
}
try {
  const sources = await lib(page, 'Sources')
  if (!sources.length) await lib(page, 'AddSources', [source])
  await lib(page, 'SetPauseBusy', false)
  await lib(page, 'SetIdleOnly', false)
  await lib(page, 'SetPaused', false)
  await lib(page, 'SetVideos', true)
  for (let i = 0; i < 90; i++) {
    const progress = await lib(page, 'Progress')
    if (progress.total >= 16 && !progress.scanning && !progress.metadata && !progress.thumbnail) break
    await sleep(500)
  }
  await lib(page, 'SetPaused', true)
  await page.reload()
  await page.waitForSelector('[id^="cell-"]')
  await page.click('[id^="cell-"]')
  const fileButton = await page.evaluateHandle(() =>
    [...document.querySelectorAll('button')].find((e) => e.textContent.trim() === 'File tools'),
  )
  await fileButton.asElement().click()
  await page.waitForSelector('[aria-label="File operation"]')
  await page.click('[aria-label="File operation"]')
  await sleep(500)
  const state = await page.evaluate(() => {
    const list = document.querySelector('[role="listbox"][data-state="open"]')
    const dialog = document.querySelector('[role="dialog"][data-state="open"]')
    const describe = (el) =>
      el && {
        tag: el.tagName,
        attributes: [...el.attributes].map((a) => [a.name, a.value]),
        z: getComputedStyle(el).zIndex,
        position: getComputedStyle(el).position,
        rect: el.getBoundingClientRect().toJSON(),
      }
    const rect = list?.getBoundingClientRect()
    return {
      list: describe(list),
      parent: describe(list?.parentElement),
      grandparent: describe(list?.parentElement?.parentElement),
      dialog: describe(dialog),
      hit: rect && describe(document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2)),
      options: [...(list?.querySelectorAll('[role="option"]') ?? [])].map((e) => e.textContent),
    }
  })
  await page.screenshot({ path: join(s.out, 'dropdown.png') })
  await listVisible('all five file operations are visible and receive pointer hits', 5)
  for (const option of [
    'Export JPEG',
    'Export PNG',
    'Move selected copies',
    'Rename selected copy',
    'Copy originals',
  ]) {
    if (!(await page.$('[role="listbox"][data-state="open"]'))) await page.click('[aria-label="File operation"]')
    await choose(option)
    check(
      `select ${option}`,
      await page.$eval('[aria-label="File operation"]', (e, text) => e.textContent.includes(text), option),
    )
  }
  await page.click('[aria-label="File operation"]')
  await resize(page, 1040, 720)
  await listVisible('dropdown stays above the modal after window resizing', 5)
  await resize(page, 520, 720)
  await listVisible('all file operations remain visible at a narrow window width', 5)
  await resize(page, 1040, 720)
  await page.keyboard.press('End')
  await page.keyboard.press('Enter')
  await sleep(200)
  check(
    'keyboard selection chooses rename and changes the input',
    !!(await page.$('input[aria-label="New file name, including extension"]')),
  )
  await page.click('[aria-label="File operation"]')
  await page.keyboard.press('Escape')
  await sleep(200)
  check(
    'Escape closes only the list and restores trigger focus',
    await page.evaluate(
      () =>
        !document.querySelector('[role="listbox"][data-state="open"]') &&
        !!document.querySelector('[role="dialog"][data-state="open"]') &&
        document.activeElement?.getAttribute('aria-label') === 'File operation',
    ),
  )
  await page.keyboard.press('Escape')
  await sleep(250)
  const ids = (await lib(page, 'Selection', { filter: '', tag: 0, person: 0, album: 0, place: 0, event: 0 }, [], false))
    .ids
  const details = await Promise.all(ids.map((id) => lib(page, 'Asset', id)))
  const video = details.find((e) => e.ext === '.mp4')
  await page.click(`#cell-${video.id}`, { count: 2 })
  await page.waitForSelector('.video-player video')
  await page.click('.video-player button[aria-label="Fullscreen"]')
  await page.waitForFunction(() => !!document.fullscreenElement)
  await page.click('.video-player button[aria-label="Save current frame"]')
  await page.waitForSelector('[aria-label="File operation"]')
  await page.click('[aria-label="File operation"]')
  await listVisible('fullscreen frame export offers two visible formats above its modal', 2)
  check(
    'fullscreen contains the dropdown portal',
    await page.evaluate(() => !!document.fullscreenElement.querySelector('[role="listbox"][data-state="open"]')),
  )
  await page.screenshot({ path: join(s.out, 'fullscreen-dropdown.png') })
  await choose('Export PNG')
  check(
    'fullscreen PNG selection updates operation',
    await page.$eval('[aria-label="File operation"]', (e) => e.textContent.includes('Export PNG')),
  )
  await page.keyboard.press('Escape')
  await sleep(300)
  await page.click('.video-player button[aria-label="Exit fullscreen"]')
  await page.waitForFunction(() => !document.fullscreenElement)
  // Resizing the window under an open menu makes WebView2 report "ResizeObserver
  // loop completed with undelivered notifications": a layout notice, counted
  // in the report, not an exception of the app.
  const notices = s.appErrors().filter((e) => e.includes('ResizeObserver loop'))
  const failures = s.appErrors().filter((e) => !e.includes('ResizeObserver loop'))
  if (notices.length) console.log(`NOTE  ${notices.length} ResizeObserver loop notices while resizing`)
  s.check('no uncaught frontend errors', failures.length === 0, failures.join(' || '))
} catch (e) {
  s.check('the file tools ran to the end', false, String(e))
  await page.screenshot({ path: join(s.out, 'failure.png') }).catch(() => {})
} finally {
  await close(app)
  s.finish()
}
