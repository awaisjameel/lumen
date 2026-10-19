import { onBeforeUnmount, shallowRef, watch } from 'vue'
import type { Ref, ShallowRef } from 'vue'
import { errorText, logError } from './errors'

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
  const state = useSuggestionState(text, fetch, none, delay)
  watch(state.error, (error) => {
    if (error) logError(error)
  })
  return state.found
}

/** Suggestions with recoverable feedback for fields that can show read failures. */
export function useSuggestionState<T>(text: Ref<string>, fetch: (query: string) => Promise<T>, none: T, delay = 120) {
  const found = shallowRef(none)
  const loading = shallowRef(false)
  const error = shallowRef('')
  let timer = 0
  let asked = 0
  let disposed = false
  function refresh() {
    clearTimeout(timer)
    const n = ++asked
    const v = text.value.trim()
    found.value = none
    error.value = ''
    loading.value = false
    if (!v.trim()) {
      return
    }
    loading.value = true
    timer = window.setTimeout(async () => {
      try {
        const got = await fetch(v)
        if (!disposed && n === asked) found.value = got
      } catch (e) {
        if (!disposed && n === asked) error.value = errorText(e)
      } finally {
        if (!disposed && n === asked) loading.value = false
      }
    }, delay)
  }
  watch(text, refresh, { immediate: true, flush: 'sync' })
  onBeforeUnmount(() => {
    disposed = true
    clearTimeout(timer)
  })
  return { found, loading, error, retry: refresh }
}
