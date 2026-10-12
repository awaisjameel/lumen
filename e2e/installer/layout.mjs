// Exercises the bootstrap and stable launcher in an entirely scratch installation.
// It writes no uninstall registry entry and never runs NSIS on the host account.
import { spawn, spawnSync, execFileSync } from 'node:child_process'
import { existsSync, linkSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { close, launch, sleep, updates } from '../lib/app.mjs'
import { ROOT, WORK } from '../lib/suite.mjs'

const run = join(WORK, `layout-${Date.now()}`)
const installation = join(run, 'Été Lumen')
mkdirSync(installation, { recursive: true })
function copyLinks(source, destination) {
  mkdirSync(destination, { recursive: true })
  for (const entry of readdirSync(source, { withFileTypes: true })) {
    if (entry.isSymbolicLink()) throw new Error('layout fixture refuses links')
    if (entry.isDirectory()) copyLinks(join(source, entry.name), join(destination, entry.name))
    else linkSync(join(source, entry.name), join(destination, entry.name))
  }
}
copyLinks(join(ROOT, 'bin', 'update-package'), installation)
linkSync(join(ROOT, 'bin', 'Lumen-launcher.exe'), join(installation, 'Lumen.exe'))
const helper = join(installation, 'lumen-update.exe')
const checks = []
function check(name, pass, detail = '') {
  checks.push({ name, pass: !!pass, detail })
  console.log(`${pass ? 'PASS' : 'FAIL'} ${name}`)
  if (!pass) throw new Error(name)
}
function command(...args) {
  return spawnSync(helper, args, { windowsHide: true, timeout: 120000, encoding: 'utf8' })
}

let app
let launcher
let launcherChild
try {
  check('bootstrap validates the complete scratch runtime', command('--bootstrap', installation).status === 0)
  const identity = JSON.parse(readFileSync(join(installation, '.lumen-install.json'), 'utf8'))
  check(
    'bootstrap records the custom Unicode installation identity',
    identity.root === installation && identity.id.length === 32,
  )
  check(
    'repeated bootstrap preserves installation identity',
    command('--bootstrap', installation).status === 0 &&
      JSON.parse(readFileSync(join(installation, '.lumen-install.json'), 'utf8')).id === identity.id,
  )
  const active = JSON.parse(readFileSync(join(installation, '.lumen-active.json'), 'utf8'))
  const executable = join(installation, 'runtime', active.generation, 'lumen-app.exe')
  app = await launch({ exe: executable, dataDir: join(run, 'data'), port: 9414 })
  await app.page.waitForSelector('.welcome, .shell, .scroller', { timeout: 60000 })
  const status = await updates(app.page, 'Status')
  check('installed app recognises managed ownership', status.managed && status.coordinator)
  check('managed unsigned fixture still refuses public delivery', !status.configured && !status.policy.automatic)
  check(
    'install admission refuses an open library without killing it',
    command('--prepare-install', installation).status !== 0 && app.proc.exitCode === null,
  )
  await close(app)
  app = undefined

  // The stable launcher clears development overrides. Observe only the exact
  // scratch executable to close its child, never another Lumen on the machine.
  const env = { ...process.env, LUMEN_DATA_DIR: join(run, 'launcher-data') }
  delete env.LUMEN_RUNTIME_DIR
  delete env.LUMEN_DEVTOOLS_PORT
  launcher = spawn(join(installation, 'Lumen.exe'), [], { env, windowsHide: true, stdio: 'ignore' })
  for (let i = 0; i < 80 && !launcherChild; i++) {
    const output = execFileSync(
      'powershell.exe',
      [
        '-NoProfile',
        '-NonInteractive',
        '-Command',
        '$p=Get-Process -Name lumen-app -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $env:LUMEN_LAYOUT_EXECUTABLE }; if($p){$p.Id}; exit 0',
      ],
      { windowsHide: true, env: { ...process.env, LUMEN_LAYOUT_EXECUTABLE: executable }, encoding: 'utf8' },
    ).trim()
    if (output && /^\d+$/.test(output)) launcherChild = output
    else await sleep(100)
  }
  check('stable launch path starts the selected complete generation', !!launcherChild && launcher.exitCode === null)
  execFileSync('taskkill.exe', ['/PID', launcherChild], { windowsHide: true, stdio: 'ignore' })
  launcherChild = undefined
  for (let i = 0; i < 100 && launcher.exitCode === null; i++) await sleep(100)
  check('launcher waits for graceful application exit', launcher.exitCode === 0)

  const unrelated = join(installation, 'runtime', active.generation, 'unrelated.txt')
  writeFileSync(unrelated, 'keep this file')
  check(
    'installation admission closes safely after all libraries exit',
    command('--prepare-install', installation).status === 0,
  )
  check(
    'generation removal succeeds without recursive deletion',
    command('--remove-generations', installation).status === 0,
  )
  check('generation cleanup preserves unrelated files', readFileSync(unrelated, 'utf8') === 'keep this file')
  check('generation cleanup removes inventoried application bytes', !existsSync(executable))
} finally {
  if (app) await close(app)
  if (launcherChild) execFileSync('taskkill.exe', ['/PID', launcherChild], { windowsHide: true, stdio: 'ignore' })
  writeFileSync(join(run, 'report.json'), JSON.stringify({ installation, checks }, null, 2))
}
