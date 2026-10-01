import { MAX_NAME_LEN } from '../bridge'

/** Counts trimmed Unicode code points, matching Go's name validation. */
export function nameLength(name: string): number {
  return Array.from(name.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')).length
}

export function nameTooLong(name: string): boolean {
  return nameLength(name) > MAX_NAME_LEN
}
