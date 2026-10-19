import { onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import type { GridItem, View } from '../bridge'
import { engine, isCancelled } from '../bridge'
import { onBlocksChanged } from './block'
import { errorText } from './errors'

export type PreviewItem = GridItem & { day: string }

/** A small, on-demand sample from a collection's newest day, using visibility-checked reads. */
export function useCollectionPreview(view: () => View) {
  const items = shallowRef<PreviewItem[]>([])
  const loading = ref(false)
  const error = ref('')
  let request: AbortController | undefined
  let revision = 0
  let loaded = false

  function clear() {
    revision++
    request?.abort()
    request = undefined
    items.value = []
    loading.value = false
    error.value = ''
    loaded = false
  }

  async function load() {
    if (loaded || loading.value) return
    const current = ++revision
    request = new AbortController()
    const signal = request.signal
    loading.value = true
    error.value = ''
    try {
      const scope = view()
      const buckets = await engine.timeline(scope, signal)
      const day = buckets[0]?.day
      const days = day ? await engine.days(day, day, scope, signal) : []
      if (current !== revision) return
      items.value = days.flatMap((d) => d.items.map((item) => ({ ...item, day: d.day }))).slice(0, 5)
      loaded = true
    } catch (e) {
      if (current === revision && !isCancelled(e)) error.value = errorText(e)
    } finally {
      if (current === revision) loading.value = false
    }
  }

  // Renaming or pinning replaces the card's payload without changing its scope.
  watch(() => JSON.stringify(view()), clear)
  // Clear before a post-block read can fail, including an already open preview.
  onBeforeUnmount(
    onBlocksChanged(() => {
      clear()
      return Promise.resolve()
    }),
  )
  onBeforeUnmount(clear)
  return { items, loading, error, load, clear }
}
