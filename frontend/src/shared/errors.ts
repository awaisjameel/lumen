import { shallowRef } from 'vue'
import {
  ErrorCode,
  errorCode,
  isCancelled,
  logUIError,
  MAX_NAME_LEN,
  PIN_COOLDOWN_SECONDS,
  PIN_MAX_LEN,
  PIN_MIN_LEN,
} from '../bridge'
import type { UIError } from '../bridge'
import { t } from './i18n'
import type { MessageKey } from './i18n'

// Failures the engine names (ErrorCode) read in the UI's own words.
const codeText: Record<ErrorCode, MessageKey> = {
  [ErrorCode.NotFound]: 'error.notFound',
  [ErrorCode.NoAI]: 'error.noAI',
  [ErrorCode.InvalidName]: 'error.invalidName',
  [ErrorCode.AlbumExists]: 'error.albumExists',
  [ErrorCode.InvalidAlbum]: 'error.invalidAlbum',
  [ErrorCode.AlbumMediaKind]: 'error.albumMediaKind',
  [ErrorCode.AlbumSelection]: 'error.albumSelection',
  [ErrorCode.AlbumDate]: 'error.albumDate',
  [ErrorCode.AlbumDateOrder]: 'error.albumDateOrder',
  [ErrorCode.AlbumSmartItems]: 'error.albumSmartItems',
  [ErrorCode.AlbumKind]: 'error.albumKind',
  [ErrorCode.AlbumReference]: 'error.albumReference',
  [ErrorCode.AlbumManualOnly]: 'error.albumManualOnly',
  [ErrorCode.PersonExists]: 'error.personExists',
  [ErrorCode.BlockedName]: 'error.blockedName',
  [ErrorCode.TagExists]: 'error.tagExists',
  [ErrorCode.BuiltinTag]: 'error.builtinTag',
  [ErrorCode.TagInAlbum]: 'error.tagInAlbum',
  [ErrorCode.AlreadyCovered]: 'error.alreadyCovered',
  [ErrorCode.FolderNotFound]: 'error.folderNotFound',
  [ErrorCode.InvalidPIN]: 'error.invalidPIN',
  [ErrorCode.PINExists]: 'error.pinExists',
  [ErrorCode.PINRateLimited]: 'error.pinRateLimited',
  [ErrorCode.BlocksLocked]: 'error.blocksLocked',
  [ErrorCode.UndoExpired]: 'error.undoExpired',
  [ErrorCode.InvalidBlock]: 'error.invalidBlock',
  [ErrorCode.DeleteFailed]: 'error.deleteFailed',
  [ErrorCode.XMPOff]: 'error.xmpOff',
  [ErrorCode.InvalidTemplate]: 'error.invalidTemplate',
  [ErrorCode.OrganiseBusy]: 'error.organiseBusy',
  [ErrorCode.OrganiseState]: 'error.organiseState',
  [ErrorCode.SigninFailed]: 'error.signinFailed',
  [ErrorCode.SigninUnavailable]: 'error.signinUnavailable',
}

/** A user-facing message for a failed engine call or other thrown value. */
export function errorText(e: unknown): string {
  const msg = e instanceof Error ? e.message : String(e)
  const code = errorCode(e)
  return code
    ? t(codeText[code], {
        max: MAX_NAME_LEN,
        pinMin: PIN_MIN_LEN,
        pinMax: PIN_MAX_LEN,
        pinCooldown: PIN_COOLDOWN_SECONDS,
      })
    : t('error.generic', { msg })
}

/**
 * For failures in work the user didn't directly ask for (loading a day,
 * prioritising what's on screen): the UI retries on its own, so they go to
 * the console and Lumen's log rather than interrupting.
 */
export function logError(e: unknown): void {
  console.error(e)
  toEngineLog(e, 'logError')
}

/**
 * For failures nothing else caught (a bug): logged, and, when notify is
 * set, the app-wide notice says something went wrong. Cancelled reads are
 * not failures.
 */
export function reportUnexpected(e: unknown, where: string, notify = true): void {
  if (isCancelled(e)) return
  console.error(e)
  toEngineLog(e, where)
  if (notify) showNotice(t('error.unexpected'))
}

/** A failure as Lumen's log records it. */
function describeError(e: unknown, where: string): UIError {
  if (!(e instanceof Error)) return { message: String(e), stack: '', where }
  const code = errorCode(e)
  return { message: `${e.name}: ${e.message}${code ? ` (${code})` : ''}`, stack: e.stack ?? '', where }
}

// The same failure from the same place is sent to the log once a minute at
// most: a view retrying every second would otherwise fill it.
const REPEAT_MS = 60_000
const sentAt = new Map<string, number>()

function toEngineLog(e: unknown, where: string): void {
  const report = describeError(e, where)
  const key = `${where}\n${report.message}`
  const now = Date.now()
  const last = sentAt.get(key)
  if (last !== undefined && now - last < REPEAT_MS) return
  if (sentAt.size > 100) sentAt.clear()
  sentAt.set(key, now)
  logUIError(report)
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
  showNotice(errorText(e))
}

/** Shows text in the app-wide notice for a few seconds. */
function showNotice(text: string): void {
  notice.value = text
  clearTimeout(noticeTimer)
  noticeTimer = window.setTimeout(() => (notice.value = ''), 6000)
}
