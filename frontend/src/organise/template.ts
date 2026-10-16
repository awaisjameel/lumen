import { ORGANISE_FALLBACKS, ORGANISE_TOKENS } from '../bridge'
import { trimName } from '../shared/names'

// The editor's example is drawn here from one real photo, so the user sees
// what a scheme does as they type. It follows the rules of fileops (the
// engine), which has the last word: the preview lists the real destinations.
// template.parity.json holds cases both this and the engine's tests check.

/** The values of one real photo. */
export interface Example {
  takenAt: number // wall-clock seconds
  event: string // "" for none
  place: string
  camera: string
  path: string // one of its files
}

// The engine writes English month names whatever the interface language.
const monthName = new Intl.DateTimeFormat('en-GB', { month: 'long', timeZone: 'UTC' })

const forbidden = '<>:"|?*/\\'
// Go's unicode.IsSpace, which differs from \s at U+0085 and U+FEFF.
const space = /[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]/u
const valueLimit = 80
const stemLimit = 120
const nameLimit = 255
const collisionRoom = ' (9999)'.length // the longest suffix fileops adds to a name already taken
const fallbackName = '_'
const utf8 = new TextEncoder()

/** A value fit for a Windows name: forbidden characters become "_", spaces are tidied, the ends lose dots. */
function clean(text: string, limit: number, fallback: string): string {
  let out = ''
  for (const ch of text) {
    const code = ch.codePointAt(0) ?? 0
    if (space.test(ch)) out += ' '
    else if (forbidden.includes(ch) || code < 32 || code === 0x7f) out += '_'
    else out += ch
  }
  const tidy = Array.from(out.split(' ').filter(Boolean).join(' '))
    .slice(0, limit)
    .join('')
    .replace(/^[. ]+|[. ]+$/g, '')
  return tidy || fallback
}

const devices = new Set([
  'CON',
  'PRN',
  'AUX',
  'NUL',
  ...[1, 2, 3, 4, 5, 6, 7, 8, 9].flatMap((n) => [`COM${n}`, `LPT${n}`]),
])

/** Whether Windows keeps a name for a device, alone or before an extension ("con", "COM1.txt"). */
function isReserved(name: string): boolean {
  const stem = name.split('.')[0] ?? ''
  return devices.has(stem.replace(/ +$/, '').toUpperCase())
}

/** Whether the scanner leaves a name out (scanner.SkipName); the case folds as strings.EqualFold does. */
function skipped(name: string): boolean {
  return (
    name.startsWith('.') || name.startsWith('$') || name.toUpperCase().toLowerCase() === 'system volume information'
  )
}

/**
 * The engine's last pass over a name that ext will follow (fileops safeName):
 * short enough for ext and a collision suffix, no trailing dots or spaces,
 * not empty, and no device name or name the scanner would skip.
 */
function safeName(name: string, ext: string): string {
  const chars = Array.from(name)
  const room = nameLimit - utf8.encode(ext).length - collisionRoom
  const safe = (chars.length > room ? chars.slice(0, room).join('') : name).replace(/[. ]+$/, '')
  if (!safe) return fallbackName
  return isReserved(safe + ext) || skipped(safe) ? `_${safe}` : safe
}

function isToken(name: string): name is (typeof ORGANISE_TOKENS)[number] {
  return ORGANISE_TOKENS.some((token) => token === name)
}

/** The names in {braces} that are not tokens, once each. */
export function unknownTokens(template: string): string[] {
  const unknown = new Set<string>()
  for (const [, name = ''] of template.matchAll(/\{([^{}]*)\}/g)) {
    if (!isToken(name)) unknown.add(name)
  }
  return [...unknown]
}

/** The folders and file name a scheme gives the example photo, joined with backslashes as Windows shows them. */
export function renderExample(template: string, example: Example): string {
  const taken = new Date(example.takenAt * 1000)
  const file = example.path.split(/[\\/]/).pop() ?? ''
  const dot = file.lastIndexOf('.')
  const stem = dot < 0 ? file : file.slice(0, dot)
  const ext = dot < 0 ? '' : file.slice(dot)
  const value = (token: (typeof ORGANISE_TOKENS)[number]): string => {
    switch (token) {
      case 'Year':
        return String(taken.getUTCFullYear()).padStart(4, '0')
      case 'Month':
        return String(taken.getUTCMonth() + 1).padStart(2, '0')
      case 'MonthName':
        return monthName.format(taken)
      case 'Day':
        return String(taken.getUTCDate()).padStart(2, '0')
      case 'Event':
        return clean(example.event, valueLimit, ORGANISE_FALLBACKS.Event)
      case 'Place':
        return clean(example.place, valueLimit, ORGANISE_FALLBACKS.Place)
      case 'Camera':
        return clean(example.camera, valueLimit, ORGANISE_FALLBACKS.Camera)
      case 'Name':
        return clean(stem, stemLimit, fallbackName)
      case 'Ext':
        return clean(ext.replace(/^\./, '').toLowerCase(), valueLimit, 'none')
    }
  }
  const parts = template
    .split(/[\\/]/)
    .filter((part) => part.trim() !== '')
    .map((part) => part.replace(/\{([^{}]*)\}/g, (whole, name: string) => (isToken(name) ? value(name) : whole)))
  const last = parts.pop()
  if (last === undefined) return ''
  return [...parts.map((dir) => safeName(dir, '')), safeName(last, ext) + ext].join('\\')
}

const templateLimit = 200

/** Whether Windows forbids a character the user typed in a name (fileops forbiddenRune). */
function forbiddenTyped(ch: string): boolean {
  const code = ch.codePointAt(0) ?? 0
  return '<>:"|?*'.includes(ch) || code < 32 || code === 0x7f
}

/** One run of literal text, or one token, of a folder or file name in a scheme. */
interface Segment {
  text: string
  token: boolean
}

/** One name of a scheme in segments, or null where fileops parseComponent refuses it. */
function parseComponent(comp: string): Segment[] | null {
  if (!trimName(comp)) return null
  const segments: Segment[] = []
  let literal = ''
  for (let rest = comp; rest !== '';) {
    const ch = String.fromCodePoint(rest.codePointAt(0) ?? 0)
    if (ch === '{') {
      const end = rest.indexOf('}')
      const name = end < 0 ? '' : rest.slice(1, end)
      if (end < 0 || !isToken(name)) return null
      if (literal) segments.push({ text: literal, token: false })
      literal = ''
      segments.push({ text: name, token: true })
      rest = rest.slice(end + 1)
      continue
    }
    if (ch === '}' || forbiddenTyped(ch)) return null
    literal += ch
    rest = rest.slice(ch.length)
  }
  if (literal) segments.push({ text: literal, token: false })
  const first = segments[0]
  const last = segments[segments.length - 1]
  if (!first || !last) return null
  const tokens = segments.some((segment) => segment.token)
  if (!tokens && comp.replace(/^[. ]+|[. ]+$/g, '') === '') return null
  if (!last.token && /[. ]$/.test(last.text)) return null
  if (!tokens && isReserved(comp)) return null
  if ((!first.token && skipped(first.text)) || (!tokens && skipped(comp))) return null
  return segments
}

/**
 * Whether the engine will accept a scheme: the rules of fileops ParseTemplate,
 * so the editor can say a scheme cannot be used before asking for a preview.
 * The engine checks again when it plans.
 */
export function validTemplate(template: string): boolean {
  if (!trimName(template) || Array.from(template).length > templateLimit) return false
  if (/^[\\/]|^[a-z]:/i.test(template)) return false
  const names = template.split(/[\\/]/).map(parseComponent)
  const last = names[names.length - 1]
  if (!last || names.some((segments) => segments === null)) return false
  // The file keeps its own extension, so a scheme that spells it out would give "IMG.jpg.jpg".
  const ext = last[last.length - 1]
  const before = last[last.length - 2]
  return !(ext?.token && ext.text === 'Ext' && before && !before.token && before.text.endsWith('.'))
}
