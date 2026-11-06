// What every suite starts from: where things are, its own DevTools port and
// folders, and one way of recording checks. A suite runs on its own
// (`node suites/viewer.mjs`) or through run.mjs, which reads its report.json.
import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { errorRecorder } from './errors.mjs'

export const E2E = resolve(dirname(fileURLToPath(import.meta.url)), '..')
export const ROOT = resolve(E2E, '..')
/** Generated fixtures, scratch libraries and results: delete it whenever you like. */
export const WORK = process.env.LUMEN_E2E_WORK ? resolve(process.env.LUMEN_E2E_WORK) : join(E2E, '.work')
/** Downloads worth keeping between runs (the camera RAW samples). */
export const CACHE = process.env.LUMEN_E2E_CACHE ? resolve(process.env.LUMEN_E2E_CACHE) : join(E2E, '.cache')
export const EXE = process.env.LUMEN_EXE ? resolve(process.env.LUMEN_EXE) : join(ROOT, 'bin', 'Lumen.exe')
export const FFMPEG = join(ROOT, 'deps', 'runtime', 'ffmpeg', 'ffmpeg.exe')

/** The application version, from its one source (build/config.yml). */
export const version = () =>
  /^\s*version:\s*"([^"]+)"/m.exec(readFileSync(join(ROOT, 'build', 'config.yml'), 'utf8'))[1]

// Each suite has its own port, so a suite never attaches to another's app.
export const SUITES = {
  onboarding: {
    port: 9401,
    about: 'an empty library opens the Welcome screen and adds nothing by itself',
  },
  tour: {
    port: 9402,
    about: 'every screen at four window widths: no sideways overflow, no errors, no request off the PC',
  },
  interface: {
    port: 9403,
    about: 'themes, command palette, keyboard, Escape, narrow windows, video playback, PIN-locked blocks',
  },
  collections: {
    port: 9404,
    about: 'albums, smart albums, Trash and Undo, custom Things, About',
  },
  'collection-recovery': {
    port: 9415,
    about: 'empty collection recovery through the PIN gate and event-name Undo across a restart',
  },
  'search-history': {
    port: 9416,
    about: 'clear recent searches, run a new query and preserve engine history through navigation and restart',
  },
  'favourite-undo': {
    port: 9417,
    about: '500-item favourite Undo: bounded calls, progress, cancellation, failed-only retry, newer edits and restart',
  },
  'undo-recovery': {
    port: 9418,
    about: 'partial album inverses, failed-only retry, correction provenance, privacy, deleted identities and restart',
  },
  'tagging-recovery': {
    port: 9419,
    about: 'annotation commit/queue faults, draft retention and owned OCR recovery through restart',
  },
  'smart-album-preview': {
    port: 9421,
    about: 'bounded smart-rule previews: real mixed media, privacy/retry, cancellation and 100,000-row parent comparison',
  },
  commands: {
    port: 9425,
    about: 'typed commands through real controls, pending/failure/retry, contextual menus, shortcuts and restart',
  },
  navigation: {
    port: 9424,
    about: 'all production destinations, typed controls, malformed input, startup, history, drawer and PIN/media boundaries',
  },
  'album-draft': {
    port: 9423,
    about: 'album draft admission across privacy Undo, failed reads, reference removal, guarded saves and navigation',
  },
  'collection-places': {
    port: 9422,
    about: 'same-count collection place corrections/Undo, privacy/read recovery and navigation ownership',
  },
  organise: {
    port: 9405,
    about: 'organise on disk: preview, move, Undo, on two isolated files',
  },
  viewer: {
    port: 9406,
    about: 'viewer zoom, rotate, compare, slideshow; selection and batch; export; rename; tracks and subtitles',
  },
  'file-tools': {
    port: 9407,
    about: 'the File tools dialog and its menu, windowed and in full screen',
  },
  branding: {
    port: 9408,
    about: 'the Lumen mark on every surface, in both themes',
  },
  formats: {
    port: 9409,
    about: 'camera RAW, JPEG XL, animated PNG, multi-page TIFF and the older video containers',
  },
  customise: {
    port: 9411,
    about: 'every preference where it takes effect, shortcuts, format exclusion, viewing cache, presets, reset',
  },
  compression: {
    port: 9412,
    about: 'verified lossless copies, partial bulk outcomes, restart, JPEG XL restoration and local presets',
  },
  updates: {
    port: 9413,
    about:
      'offline defaults, updater capabilities, settings privacy, disabled public delivery and narrow-window layout',
  },
  upgrade: {
    port: 9414,
    about: 'a previous build leaves user state and queued work for the candidate to preserve and finish',
    optIn: true,
  },
  showcase: {
    port: 9410,
    about: 'the component showcase of a development run (starts wails3 dev)',
    optIn: true,
  },
}
export const BASE_PORT = 9400
export const PERF_PORT = 9420

/**
 * Starts a suite: fresh `run` (scratch library and files) and `out` (report
 * and screenshots) folders, and a recorder of its checks.
 */
export function suite(name) {
  const entry = SUITES[name]
  if (!entry) throw new Error(`unknown suite ${name}`)
  if (!existsSync(EXE)) throw new Error(`${EXE} is missing: build the app first (wails3 build), or set LUMEN_EXE`)
  const run = join(WORK, 'runs', name)
  const out = join(WORK, 'results', name)
  for (const dir of [run, out]) {
    rmSync(dir, { recursive: true, force: true })
    mkdirSync(dir, { recursive: true })
  }
  const checks = []
  const methods = new Map()
  const bindings = join(ROOT, 'frontend', 'bindings', 'lumen', 'services')
  for (const file of readdirSync(bindings).filter(file => file.endsWith('service.ts'))) {
    const source = readFileSync(join(bindings, file), 'utf8')
    for (const match of source.matchAll(/export function (\w+)\([\s\S]*?\$Call\.ByID\((\d+)/g)) methods.set(Number(match[2]), match[1])
  }
  const diagnostics = errorRecorder(methods)
  const errors = diagnostics.records
  const check = (label, pass, detail = '') => {
    const text = typeof detail === 'string' ? detail : JSON.stringify(detail)
    checks.push({ name: label, pass: !!pass, detail: text.slice(0, 600) })
    console.log(`${pass ? 'PASS' : 'FAIL'}  ${label}${text ? `  — ${text.slice(0, 260)}` : ''}`)
    return !!pass
  }
  /** A check the rest of the suite cannot go on without. */
  const must = (label, pass, detail = '') => {
    if (!check(label, pass, detail)) throw new Error(label)
  }
  /** Runs one section; a throw is that section's failure, and the others still run. */
  const only = (process.env.SECTIONS ?? '').split(',').filter(Boolean)
  const guard = async (label, fn) => {
    if (only.length && !only.includes(label)) return
    try {
      await fn()
    } catch (e) {
      check(label, false, `threw: ${String(e).slice(0, 400)}`)
    }
  }
  const cancellableReads = new Set(['Timeline', 'Days', 'Search', 'Hits', 'TopPlaces', 'PreviewAlbum'])
  /** Only cancelled reads are normal globally; fault suites identify their own refusals. */
  const watch = (page, options = {}) => diagnostics.watch(page, {
    ...options,
    expected: row => (row.kind === 'response' && row.status === 422 && row.errorKind === 'RuntimeError' &&
      row.message === 'context canceled' && cancellableReads.has(row.method)) || !!options.expected?.(row),
  })
  const appErrors = diagnostics.unexpected
  /** Writes report.json and sets the exit code. */
  const finish = (extra = {}) => {
    const failed = checks.filter((c) => !c.pass)
    writeFileSync(
      join(out, 'report.json'),
      JSON.stringify({ suite: name, exe: EXE, checks, errors, ...extra }, null, 2),
    )
    console.log(`\n${name}: ${checks.length - failed.length}/${checks.length} passed`)
    if (failed.length || !checks.length) process.exitCode = 1
  }
  return {
    name,
    exe: EXE,
    port: entry.port,
    run,
    out,
    checks,
    errors,
    check,
    must,
    guard,
    watch,
    appErrors,
    settleErrors: diagnostics.settle,
    finish,
  }
}
