import { onBeforeUnmount } from 'vue'
import { onLibraryChanged } from '../bridge'
import { logError } from './errors'

/**
 * Calls reload now and then while the library changes (counts grow as the
 * AI works), at most every few seconds, for as long as the calling
 * component is mounted.
 */
export function useLiveReload(reload: () => Promise<unknown>, every = 3000): void {
  let timer = 0
  const stop = onLibraryChanged(() => {
    if (timer) return
    timer = window.setTimeout(() => {
      timer = 0
      reload().catch(logError)
    }, every)
  })
  onBeforeUnmount(() => {
    stop()
    clearTimeout(timer)
  })
}
