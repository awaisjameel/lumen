// Verifies an extracted installer and runs native suites on its shipped runtime.
import { createHash } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import { createReadStream, existsSync, readdirSync, mkdirSync, writeFileSync, readFileSync } from 'node:fs'
import { delimiter, join, relative, resolve } from 'node:path'
import { ROOT, version } from '../lib/suite.mjs'

const extracted = resolve(process.argv[2] ?? '')
if (!process.argv[2] || !existsSync(join(extracted, 'Lumen.exe'))) {
  throw new Error('Usage: node e2e/installer/package.mjs <extracted installer folder> [suite ...]')
}
const results = join(ROOT, 'e2e', '.work', `package-${version()}`)
mkdirSync(results, { recursive: true })
const hash = async (path) => {
  const digest = createHash('sha256')
  for await (const bytes of createReadStream(path)) digest.update(bytes)
  return digest.digest('hex')
}
function files(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name)
    if (entry.isSymbolicLink()) throw new Error(`Unexpected runtime link: ${path}`)
    return entry.isDirectory() ? files(path) : [path]
  })
}
const checks = []
const runtime = join(ROOT, 'deps', 'runtime')
const packaged = join(ROOT, 'bin', 'update-package')
const active = existsSync(join(packaged, '.lumen-active.json'))
  ? JSON.parse(readFileSync(join(packaged, '.lumen-active.json'), 'utf8'))
  : null
const expected = [
  ...files(active ? packaged : runtime).map((path) => ({
    path: relative(active ? packaged : runtime, path),
    source: path,
  })),
  ...['Lumen.exe', 'THIRD_PARTY_NOTICES.txt'].map((path) => ({
    path,
    source: join(ROOT, 'bin', active && path === 'Lumen.exe' ? 'Lumen-launcher.exe' : path),
  })),
  {
    path: join('$PLUGINSDIR', 'MicrosoftEdgeWebView2RuntimeInstallerX64.exe'),
    source: join(ROOT, 'deps', 'installer', 'MicrosoftEdgeWebView2RuntimeInstallerX64.exe'),
  },
]
for (const entry of expected) {
  const sourceHash = await hash(entry.source)
  const path = join(extracted, entry.path)
  const extractedHash = existsSync(path) ? await hash(path) : null
  checks.push({
    name: entry.path,
    pass: sourceHash === extractedHash,
    sha256: extractedHash,
  })
}
// NSIS extracts its own UI plugins temporarily; they are not runtime files.
const plugins = ['modern-wizard.bmp', 'nsDialogs.dll', 'System.dll'].map((name) => join('$PLUGINSDIR', name))
const unexpected = files(extracted)
  .map((path) => relative(extracted, path))
  .filter((path) => !expected.some((e) => e.path === path) && !plugins.includes(path))
checks.push({
  name: 'no unexpected payload files',
  pass: unexpected.length === 0,
  detail: unexpected,
})
const installer = join(ROOT, 'bin', `Lumen-${version()}-x64-setup.exe`)
writeFileSync(
  join(results, 'payload.json'),
  JSON.stringify({ version: version(), installer, sha256: await hash(installer), checks }, null, 2),
)
const failed = checks.filter((check) => !check.pass)
console.log(`Package payload: ${checks.length - failed.length}/${checks.length} passed`)
if (failed.length) throw new Error(JSON.stringify(failed))

// Remove development overrides and the runtime DLL search path before launch.
const env = {
  ...process.env,
  LUMEN_E2E_SYSTEM_PATH_ONLY: '1',
  LUMEN_EXE: active ? join(extracted, 'runtime', active.generation, 'lumen-app.exe') : join(extracted, 'Lumen.exe'),
}
delete env.LUMEN_RUNTIME_DIR
delete env.SECTIONS
const pathKey = Object.keys(env).find((key) => key.toLowerCase() === 'path')
if (pathKey)
  env[pathKey] = env[pathKey]
    .split(delimiter)
    .filter((entry) => resolve(entry).toLowerCase() !== runtime.toLowerCase())
    .join(delimiter)
const run = spawnSync(process.execPath, [join(ROOT, 'e2e', 'run.mjs'), ...process.argv.slice(3)], {
  cwd: ROOT,
  env,
  stdio: 'inherit',
})
if (run.error) throw run.error
process.exitCode = run.status ?? 1
