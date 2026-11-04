import { sleep } from './app.mjs'

/** Wait on an observable state with a specific failure instead of a blind delay. */
export async function until(test, message, timeout = 30_000) {
  const end = Date.now() + timeout
  while (Date.now() < end) {
    if (await test()) return
    await sleep(30)
  }
  throw new Error(message)
}

export async function button(page, text, root = '') {
  const selector = `${root ? `${root} ` : ''}button::-p-text(${text})`
  await page.waitForSelector(selector, { visible: true })
  for (const element of await page.$$(selector))
    if (await element.evaluate((node, text) => node.textContent.trim() === text, text)) {
      await element.asLocator().click()
      return
    }
  throw new Error(`missing button: ${text}`)
}

export async function dismissNotices(page) {
  for (const close of await page.$$('button[aria-label="Dismiss notification"]')) await close.asLocator().click()
}

export async function selectAllResults(page) {
  // Portalled controls animate: wait for stable geometry before a real click.
  await page.locator('button[aria-label="Selection options"]').click()
  await page.locator('[role="menuitem"]::-p-text(Select all results)').click()
}
