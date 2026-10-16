import {
  ErrorCode,
  errorCode,
  isCancelled,
  isErrorCode,
  logUIError,
  MAX_NAME_LEN,
  PIN_COOLDOWN_SECONDS,
  PIN_MAX_LEN,
  PIN_MIN_LEN,
} from '../bridge'
import type { UIError } from '../bridge'
import { t } from './i18n'
import type { MessageKey } from './i18n'
import { notify } from './notify'

// Failures the engine names (ErrorCode) read in the UI's own words.
const codeText: Record<ErrorCode, MessageKey> = {
  [ErrorCode.CorrectionSelection]: 'error.correctionSelection',
  [ErrorCode.CorrectionDate]: 'error.correctionDate',
  [ErrorCode.CorrectionLocation]: 'error.correctionLocation',
  [ErrorCode.InvalidFilter]: 'error.invalidFilter',
  [ErrorCode.UpdateUnavailable]: 'update.unavailable',
  [ErrorCode.UpdateBusy]: 'update.busy',
  [ErrorCode.UpdateInvalid]: 'update.invalid',
  [ErrorCode.UpdateState]: 'update.stateError',
  [ErrorCode.UpdateSpace]: 'update.space',
  [ErrorCode.UpdatePublisher]: 'update.publisher',
  [ErrorCode.CompressionBusy]: 'error.compressionBusy',
  [ErrorCode.CompressionState]: 'error.compressionState',
  [ErrorCode.CompressionPlan]: 'error.compressionPlan',
  [ErrorCode.Preservation]: 'error.preservation',
  [ErrorCode.CopySpace]: 'error.copySpace',
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
  [ErrorCode.TrashChanged]: 'error.trashChanged',
  [ErrorCode.XMPOff]: 'error.xmpOff',
  [ErrorCode.InvalidTemplate]: 'error.invalidTemplate',
  [ErrorCode.OrganiseBusy]: 'error.organiseBusy',
  [ErrorCode.OrganiseState]: 'error.organiseState',
  [ErrorCode.SelectionChanged]: 'error.selectionChanged',
  [ErrorCode.Stopping]: 'error.stopping',
  [ErrorCode.InvalidFileName]: 'error.invalidFileName',
  [ErrorCode.DestinationOutside]: 'error.destinationOutside',
  [ErrorCode.DestinationFolder]: 'organise.reason.not_a_folder',
  [ErrorCode.DestinationExcluded]: 'error.destinationExcluded',
  [ErrorCode.DestinationBlocked]: 'organise.reason.blocked_folder',
  [ErrorCode.DestinationTaken]: 'organise.reason.taken',
  [ErrorCode.SourceChanged]: 'organise.reason.changed',
  [ErrorCode.Unsupported]: 'error.unsupported',
  [ErrorCode.PresetInvalid]: 'error.presetInvalid',
  [ErrorCode.PresetNewer]: 'error.presetNewer',
  [ErrorCode.SigninFailed]: 'error.signinFailed',
  [ErrorCode.SigninUnavailable]: 'error.signinUnavailable',
}

/** A user-facing message for a failed engine call or other thrown value. */
export function errorText(e: unknown): string {
  return failureText(errorCode(e), e instanceof Error ? e.message : String(e))
}

/**
 * errorText for a failure that was not thrown: one asset's of a selection
 * action, which arrives as the engine's code (if it has one) and message.
 */
export function failureText(code: string | undefined, msg: string): string {
  return isErrorCode(code)
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
 * For failures nothing else caught (a bug): logged, and, when tell is set,
 * a notification says something went wrong. Cancelled reads are not
 * failures.
 */
export function reportUnexpected(e: unknown, where: string, tell = true): void {
  if (isCancelled(e)) return
  console.error(e)
  toEngineLog(e, where)
  if (tell) showError(t('error.unexpected'))
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

/** The id of the one failure notification: a newer failure replaces the one showing. */
export const ERROR_NOTIFICATION = 'error'

/**
 * For failures of something the user did that has no place of its own to
 * say so (the favourite heart, the pause button): kept until dismissed.
 */
export function reportError(e: unknown): void {
  logError(e)
  showError(errorText(e))
}

/** Keeps a failure notification visible until explicitly dismissed. */
function showError(text: string): void {
  notify({ id: ERROR_NOTIFICATION, kind: 'error', title: text, duration: 0 })
}
