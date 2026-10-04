import { ORGANISE_FALLBACKS, ORGANISE_TOKENS } from '../bridge'

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
