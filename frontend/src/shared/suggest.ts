import { onBeforeUnmount, shallowRef, watch } from 'vue'
import type { Ref, ShallowRef } from 'vue'
import { logError } from './errors'

/**
 * Suggestions for what is being typed: fetch runs once typing pauses for
 * delay ms, and only the answer for the latest text is kept (an older one
 * arriving late is dropped). Blank text suggests none. For as long as the
 * calling component is mounted.
 */
export function useSuggestions<T>(
  text: Ref<string>,
  fetch: (query: string) => Promise<T>,
  none: T,
  delay = 120,
): Readonly<ShallowRef<T>> {
  const found = shallowRef(none)
  let timer = 0
  let asked = 0
  watch(text, (v) => {
    clearTimeout(timer)
    const n = ++asked
    if (!v.trim()) {
      found.value = none
      return
    }
    timer = window.setTimeout(async () => {
      try {
        const got = await fetch(v)
        if (n === asked) found.value = got
      } catch (e) {
        logError(e)
      }
    }, delay)
  })
  onBeforeUnmount(() => clearTimeout(timer))
  return found
}
