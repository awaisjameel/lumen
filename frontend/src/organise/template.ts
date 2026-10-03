import { ORGANISE_FALLBACKS, ORGANISE_TOKENS } from '../bridge'

// The editor's example is drawn here from one real photo, so the user sees
// what a scheme does as they type. It follows the rules of fileops (the
// engine), which has the last word: the preview lists the real destinations.

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
const valueLimit = 80
const stemLimit = 120

/** A value fit for a Windows name: forbidden characters become "_", spaces are tidied, the ends lose dots. */
function clean(text: string, limit: number, fallback: string): string {
  let out = ''
  for (const ch of text) {
    if (/\s/.test(ch)) out += ' '
    else if (forbidden.includes(ch) || ch.charCodeAt(0) < 32) out += '_'
    else out += ch
  }
  const tidy = Array.from(out.split(' ').filter(Boolean).join(' '))
    .slice(0, limit)
    .join('')
    .replace(/^[. ]+|[. ]+$/g, '')
  return tidy || fallback
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
        return clean(stem, stemLimit, '_')
      case 'Ext':
        return clean(ext.replace(/^\./, '').toLowerCase(), valueLimit, 'none')
    }
  }
  const parts = template
    .split(/[\\/]/)
    .filter((part) => part.trim() !== '')
    .map((part) => part.replace(/\{([^{}]*)\}/g, (whole, name: string) => (isToken(name) ? value(name) : whole)))
  if (parts.length === 0) return ''
  parts[parts.length - 1] = `${parts[parts.length - 1] ?? ''}${ext}`
  return parts.join('\\')
}
