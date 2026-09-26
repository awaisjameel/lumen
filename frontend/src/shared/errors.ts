import { t } from './i18n'

/** A user-facing message for a failed engine call or other thrown value. */
export function errorText(e: unknown): string {
  return t('error.generic', { msg: e instanceof Error ? e.message : String(e) })
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
