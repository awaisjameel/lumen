import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'

/**
 * Whether a settings row is shown for the page's search: true when any of
 * the row's texts holds what was typed, and always while nothing is.
 */
export function useRowFilter(query: () => string): (...keys: MessageKey[]) => boolean {
  return (...keys) => {
    const needle = query().trim().toLocaleLowerCase()
    return !needle || keys.some((key) => t(key).toLocaleLowerCase().includes(needle))
  }
}
