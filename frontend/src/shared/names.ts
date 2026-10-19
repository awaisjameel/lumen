import { MAX_NAME_LEN } from '../bridge'
import { LOCALE } from './i18n'

const byName = new Intl.Collator(LOCALE, { numeric: true })

/** Names use the same numeric, accent-aware order as core.CompareNames. */
export function compareNames(a: string, b: string): number {
  return byName.compare(a, b) || (a < b ? -1 : a > b ? 1 : 0)
}

/** Unicode simple folding, as core.Fold uses, without treating the filter as a pattern. */
export function nameIncludes(name: string, query: string): boolean {
  return new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'iu').test(name)
}

/**
 * name without the leading and trailing whitespace Go's strings.TrimSpace
 * removes: JavaScript's trim also strips U+FEFF, which Go keeps, and keeps
 * U+0085, which Go strips, so a name trimmed here is the one the engine saves.
 */
export function trimName(name: string): string {
  return name.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
}

/** Counts trimmed Unicode code points, matching Go's name validation. */
export function nameLength(name: string): number {
  return Array.from(trimName(name)).length
}

export function nameTooLong(name: string): boolean {
  return nameLength(name) > MAX_NAME_LEN
}
