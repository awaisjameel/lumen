import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'

const synonyms: Partial<Record<MessageKey, string>> = {
  'settings.theme': 'daylight darkroom night',
  'settings.accent': 'color cobalt',
  'settings.textSize': 'font zoom readability',
  'settings.threadsLabel': 'cpu performance speed processor',
  'settings.ocr': 'ocr recognition text scan',
  'settings.formats': 'jpeg png gif webp bmp tiff heic heif avif jpeg xl jxl raw dng',
  'viewer.subtitles': 'captions',
  'viewer.subtitleLanguage': 'captions language',
  'settings.confidence': 'accuracy threshold',
}

export function matchesSettings(query: string, ...keys: MessageKey[]): boolean {
  const needle = query.trim().toLocaleLowerCase()
  return !needle || keys.some((key) => `${t(key)} ${synonyms[key] ?? ''}`.toLocaleLowerCase().includes(needle))
}

/**
 * Whether a settings row is shown for the page's search: true when any of
 * the row's texts holds what was typed, and always while nothing is.
 */
export function useRowFilter(query: () => string): (...keys: MessageKey[]) => boolean {
  return (...keys) => matchesSettings(query(), ...keys)
}
