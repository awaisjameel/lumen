import { MAX_NAME_LEN } from '../bridge'

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
