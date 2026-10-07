// Runs the end-to-end suites against the built app, one after another (they
// share the PC's GPU and Lumen allows one instance per library), and prints
// what passed.
//   node run.mjs                  every suite (not the opt-in ones)
//   node run.mjs viewer formats   the named suites
//   node run.mjs --list           what there is
//   node run.mjs --clean          delete generated fixtures, libraries and results
import { spawnSync } from 'node:child_process'
import { existsSync, readFileSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { E2E, EXE, SUITES, WORK } from './lib/suite.mjs'

const args = process.argv.slice(2)
if (args.includes('--list')) {
  for (const [name, { about, optIn }] of Object.entries(SUITES))
    console.log(`${name.padEnd(12)} ${about}${optIn ? ' [only when named]' : ''}`)
  process.exit(0)
}
if (args.includes('--clean')) {
  rmSync(WORK, { recursive: true, force: true })
  console.log(`removed ${WORK}`)
  process.exit(0)
}
const unknown = args.filter((a) => !SUITES[a])
if (unknown.length) {
  console.error(`unknown suite: ${unknown.join(', ')} (see node run.mjs --list)`)
  process.exit(2)
}
if (!existsSync(EXE)) {
  console.error(`${EXE} is missing: build the app first (wails3 build), or set LUMEN_EXE`)
  process.exit(2)
}

const names = args.length ? args : Object.keys(SUITES).filter((n) => !SUITES[n].optIn)
const rows = []
for (const name of names) {
  console.log(`\n=== ${name}: ${SUITES[name].about}`)
  const started = Date.now()
  const { status } = spawnSync(process.execPath, [join(E2E, 'suites', `${name}.mjs`)], { stdio: 'inherit' })
  let checks = []
  try {
    checks = JSON.parse(readFileSync(join(WORK, 'results', name, 'report.json'), 'utf8')).checks
  } catch {
    /* the suite died before it could report */
  }
  const failed = checks.filter((c) => !c.pass)
  rows.push({
    name,
    ok: status === 0 && checks.length > 0 && failed.length === 0,
    passed: checks.length - failed.length,
    total: checks.length,
    failed,
    seconds: Math.round((Date.now() - started) / 1000),
  })
}

console.log('\n=== summary')
for (const r of rows) {
  console.log(
    `${r.ok ? 'PASS' : 'FAIL'}  ${r.name.padEnd(12)} ${String(r.passed).padStart(3)}/${String(r.total).padEnd(3)} ${r.seconds}s${r.total ? '' : '  (no report: the suite did not finish)'}`,
  )
  for (const c of r.failed) console.log(`        ${c.name}`)
}
console.log(`results and screenshots: ${join(WORK, 'results')}`)
process.exit(rows.every((r) => r.ok) ? 0 : 1)
