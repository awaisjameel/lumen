// Waits for a started Lumen's WebView to list its app page on the DevTools
// port. Kept free of puppeteer so the harness checks in scripts/lint.sh can
// test it without the e2e dependencies installed.

/**
 * The first launch on a fresh CI runner has taken anywhere from 3 s to over
 * 30 s (WebView2 creates its profile and the runtime DLLs come off a cold
 * disk), while later launches in the same job take seconds. The wait is
 * therefore a deadline in time, the 90 s the installer sandbox already gives
 * the installed app, and not a count of attempts whose length depends on how
 * long each request takes.
 */
export const WEBVIEW_DEADLINE_MS = 90_000
/** A DevTools port that accepts a connection and never answers must not hold the wait past its deadline. */
export const DEVTOOLS_REQUEST_MS = 2_000
const POLL_MS = 100

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** Whether target is the app's own page rather than about:blank or a worker. */
export const isAppPage = (target) => target.type === 'page' && target.url.startsWith('http')

/**
 * Polls proc's DevTools port until it lists the app page and returns the
 * targets. Fails at once if proc exits or cannot start, and at the deadline
 * with what the port last reported, so a failed run says how far the launch
 * got.
 */
export async function waitForAppPage(
  proc,
  port,
  { deadline = WEBVIEW_DEADLINE_MS, request = DEVTOOLS_REQUEST_MS } = {},
) {
  let spawnError
  proc.once('error', (error) => {
    spawnError = error
  })
  const started = Date.now()
  let seen = 'the port never answered'
  for (;;) {
    if (spawnError) throw new Error(`Lumen did not start: ${spawnError.message}`)
    // A Lumen started while another is still closing on the same library exits at once.
    if (proc.exitCode !== null) throw new Error(`Lumen exited with ${proc.exitCode} before its window opened`)
    try {
      const response = await fetch(`http://127.0.0.1:${port}/json/list`, { signal: AbortSignal.timeout(request) })
      const targets = await response.json()
      if (targets.some(isAppPage)) return targets
      seen = `the port listed ${JSON.stringify(targets.map((t) => `${t.type} ${t.url}`))}`
    } catch {
      /* not listening yet, or too slow to answer */
    }
    const waited = Date.now() - started
    if (waited >= deadline)
      throw new Error(`the WebView did not open its app page within ${Math.round(waited / 1000)} s: ${seen}`)
    await sleep(POLL_MS)
  }
}
