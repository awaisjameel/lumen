import { lib, sleep } from './app.mjs'

/** Wait on an observable state with a specific failure instead of a blind delay. */
export async function until(test, message, timeout = 30_000) {
  const end = Date.now() + timeout
  while (Date.now() < end) {
    if (await test()) return
    await sleep(30)
  }
  throw new Error(message)
}

async function clickText(page, selector, text) {
  await page.waitForSelector(selector, { visible: true })
  for (const element of await page.$$(selector))
    if (await element.evaluate((node, text) => node.textContent.trim() === text, text)) {
      await element.asLocator().click()
      return
    }
  throw new Error(`missing control: ${text}`)
}

export function button(page, text, root = '') {
  return clickText(page, `${root ? `${root} ` : ''}button::-p-text(${text})`, text)
}

/** Bounds failure diagnostics when a dead page cannot answer the protocol. */
export async function diagnostic(read) {
  let timer
  try {
    return await Promise.race([read, new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error('diagnostic timed out')), 5000)
    })])
  } catch (error) {
    return { error: String(error) }
  } finally {
    clearTimeout(timer)
  }
}


/** Exact option labels distinguish Photos from Photos and videos. */
export function option(page, text) {
  return clickText(page, `[role="option"]::-p-text(${text})`, text)
}

export async function dismissNotices(page) {
  for (const close of await page.$$('button[aria-label="Dismiss notification"]')) await close.asLocator().click()
}

export async function selectAllResults(page) {
  // Portalled controls animate: wait for stable geometry before a real click.
  await page.locator('button[aria-label="Selection options"]').click()
  await page.locator('[role="menuitem"]::-p-text(Select all results)').click()
}

/** Selects a real grid item and waits for its current inspector identity. */
export async function selectPhoto(page, id, step = () => undefined) {
  step('clear selection')
  const clear = await page.$('button[aria-label="Clear selection"]')
  if (clear) await clear.click()
  if (!(await page.$(`#cell-${id}`))) await page.$eval('.scroller', (el) => { el.scrollTop = el.scrollHeight })
  step('wait for the target cell')
  await page.waitForSelector(`#cell-${id}`)
  // Privacy recovery can finish its write before coalesced timeline reads
  // have moved the virtual rows. Take coordinates only after those settle.
  step('wait for the library reads')
  await page.waitForNetworkIdle()
  step('select the target cell')
  await page.locator(`#cell-${id}`).click()
  const details = 'button[aria-label="Details"]'
  step('open Details')
  if ((await page.$eval(details, (el) => el.getAttribute('aria-pressed'))) !== 'true') await page.locator(details).click()
  step('wait for the inspector')
  await page.waitForSelector('[data-edit-date]', { visible: true })
  const name = (await lib(page, 'Asset', id)).paths[0].split(/[\\/]/).at(-1)
  step('wait for the current inspector identity')
  await until(() => page.$eval('.preview-caption h3', (el, name) => el.textContent.trim() === name, name),
    'the inspector did not settle on the current target')
}

/** Finds the newest captured Undo, independently of other notices. */
export async function undoNotice(page) {
  await until(() => page.$$eval('[data-notification^="undo."][data-state="open"]', (rows) => rows.some((row) =>
    [...row.querySelectorAll('button')].some((button) => button.textContent.trim() === 'Undo'))), 'no captured Undo was offered')
  return page.$$eval('[data-notification^="undo."][data-state="open"]', (rows) => rows.filter((row) =>
    [...row.querySelectorAll('button')].some((button) => button.textContent.trim() === 'Undo')).at(-1).getAttribute('data-notification'))
}
