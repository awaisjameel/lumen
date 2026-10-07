// Alternating A/B comparison of two builds, so machine noise falls on both
// alike: cold start to a usable grid (N launches each) and idle memory after
// a rest on the grid (M samples each). Each build has its own indexed library
// (perf/index.mjs makes one).
//   node perf/compare.mjs <exeA> <dataA> <exeB> <dataB> [n] [m]
import { mkdirSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { close, launch, memoryMB, sleep, usableGrid } from '../lib/app.mjs'
import { PERF_PORT, WORK } from '../lib/suite.mjs'

const [exeA, dataA, exeB, dataB, n = '8', m = '3'] = process.argv.slice(2)
if (!dataB) throw new Error('usage: node perf/compare.mjs <exeA> <dataA> <exeB> <dataB> [n] [m]')
const builds = { a: [resolve(exeA), resolve(dataA)], b: [resolve(exeB), resolve(dataB)] }
const port = PERF_PORT

const out = { builds, cold: { a: [], b: [] }, idle: { a: [], b: [] } }
// One unmeasured launch each first: the first start of a new binary is scanned by Windows.
for (const [exe, dataDir] of Object.values(builds)) {
  const app = await launch({ exe, dataDir, port })
  await usableGrid(app.page)
  await close(app)
}
for (let i = 0; i < Number(n); i++) {
  for (const [name, [exe, dataDir]] of Object.entries(builds)) {
    const app = await launch({ exe, dataDir, port })
    await usableGrid(app.page)
    out.cold[name].push(Date.now() - app.started)
    await close(app)
  }
}
for (let i = 0; i < Number(m); i++) {
  for (const [name, [exe, dataDir]] of Object.entries(builds)) {
    const app = await launch({ exe, dataDir, port })
    await usableGrid(app.page)
    await sleep(45000)
    out.idle[name].push(memoryMB(app.proc.pid))
    await close(app)
  }
}
const med = (xs) => [...xs].sort((a, b) => a - b)[Math.floor(xs.length / 2)]
out.summary = {
  coldMedianMs: { a: med(out.cold.a), b: med(out.cold.b) },
  idleWorkingSetMB: { a: med(out.idle.a.map((x) => x.workingSetMB)), b: med(out.idle.b.map((x) => x.workingSetMB)) },
  idlePrivateMB: { a: med(out.idle.a.map((x) => x.privateMB)), b: med(out.idle.b.map((x) => x.privateMB)) },
}
const dir = join(WORK, 'results', 'perf')
mkdirSync(dir, { recursive: true })
writeFileSync(join(dir, 'compare.json'), JSON.stringify(out, null, 2))
console.log(JSON.stringify(out.summary, null, 2))
