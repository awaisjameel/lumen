// Indexes the library fixture into a scratch library with any build, timing
// it: the library a build is then measured on (an older build cannot open a
// library a newer one has migrated, so each build gets its own).
//   node perf/index.mjs <exe> <dataDir>
import { resolve } from 'node:path'
import { close, launch, lib, unthrottle, waitIndexed } from '../lib/app.mjs'
import { fixture } from '../lib/fixtures.mjs'
import { PERF_PORT } from '../lib/suite.mjs'

const [exe, dataDir] = process.argv.slice(2)
if (!exe || !dataDir) throw new Error('usage: node perf/index.mjs <exe> <dataDir>')
const corpus = fixture('library')
const app = await launch({ exe: resolve(exe), dataDir: resolve(dataDir), port: PERF_PORT })
const t0 = Date.now()
try {
  if (!(await lib(app.page, 'Sources'))?.length) {
    await unthrottle(app.page)
    await lib(app.page, 'AddSources', [corpus])
    // Off the Welcome screen at once, so nothing on it can be clicked.
    await app.page.reload()
  }
  await waitIndexed(app.page, { ai: true, timeout: 3_600_000, log: console.log })
  console.log(`indexed in ${((Date.now() - t0) / 1000).toFixed(0)} s`)
} finally {
  await close(app)
}
