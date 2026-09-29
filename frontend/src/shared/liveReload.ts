import { onBeforeUnmount } from 'vue'
import { onLibraryChanged } from '../bridge'
import type { Changes } from '../bridge'
import { logError } from './errors'

/**
 * Calls reload now and then while the library changes (counts grow as the
 * AI works), at most every few seconds, for as long as the calling
 * component is mounted. when says which changes matter to it (by default,
 * any): a list of people needn't reload while only tags change.
 */
export function useLiveReload(
  reload: () => Promise<unknown>,
  when: (c: Changes) => boolean = () => true,
  every = 3000,
): void {
  let timer = 0
  const stop = onLibraryChanged((c) => {
    if (timer || !when(c)) return
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
