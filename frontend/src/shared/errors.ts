import { shallowRef } from 'vue'
import { ErrorCode, errorCode, MAX_NAME_LEN } from '../bridge'
import { t } from './i18n'
import type { MessageKey } from './i18n'

// Failures the engine names (ErrorCode) read in the UI's own words.
const codeText: Record<Exclude<ErrorCode, typeof ErrorCode.InvalidAlbum>, MessageKey> = {
  [ErrorCode.NotFound]: 'error.notFound',
  [ErrorCode.NoAI]: 'error.noAI',
  [ErrorCode.InvalidName]: 'error.invalidName',
  [ErrorCode.AlbumExists]: 'error.albumExists',
  [ErrorCode.PersonExists]: 'error.personExists',
  [ErrorCode.TagExists]: 'error.tagExists',
  [ErrorCode.BuiltinTag]: 'error.builtinTag',
  [ErrorCode.TagInAlbum]: 'error.tagInAlbum',
  [ErrorCode.AlreadyCovered]: 'error.alreadyCovered',
  [ErrorCode.FolderNotFound]: 'error.folderNotFound',
}

/** A user-facing message for a failed engine call or other thrown value. */
export function errorText(e: unknown): string {
  const msg = e instanceof Error ? e.message : String(e)
  const code = errorCode(e)
  if (code === ErrorCode.InvalidAlbum) {
    // The engine says what is wrong with the rule after this prefix.
    return t('error.invalidAlbum', { why: msg.replace(/^invalid album: /, '') })
  }
  return code ? t(codeText[code], { max: MAX_NAME_LEN }) : t('error.generic', { msg })
}

/**
 * For failures in work the user didn't directly ask for (loading a day,
 * prioritising what's on screen): the UI retries on its own, so they go to
 * the console rather than interrupting.
 */
export function logError(e: unknown): void {
  console.error(e)
}

/** Await p; if it fails, log why and use fallback instead. */
export async function orFallback<T>(p: Promise<T>, fallback: T): Promise<T> {
  try {
    return await p
  } catch (e) {
    logError(e)
    return fallback
  }
}

/** The app-wide notice (shown by Notice in App), "" when there is none. */
export const notice = shallowRef('')
let noticeTimer = 0

/**
 * For failures of something the user did that has no place of its own to
 * say so (the favourite heart, the pause button): shown for a few seconds.
 */
export function reportError(e: unknown): void {
  logError(e)
  notice.value = errorText(e)
  clearTimeout(noticeTimer)
  noticeTimer = window.setTimeout(() => (notice.value = ''), 6000)
}
