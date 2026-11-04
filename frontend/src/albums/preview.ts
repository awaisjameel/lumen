import { onScopeDispose, ref, shallowRef, watch } from 'vue'
import { engine, isCancelled } from '../bridge'
import type { AlbumRule, GridItem } from '../bridge'
import { errorText } from '../shared/errors'
import { onBlocksChanged } from '../shared/block'

/** Reads the store's bounded smart-rule preview without saving the draft. */
export function useAlbumPreview(rule: () => AlbumRule | null) {
  const count = ref<number | null>(null)
  const items = shallowRef<GridItem[]>([])
  const loading = ref(false)
  const error = ref('')
  let controller: AbortController | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let revision = 0

  async function refresh() {
    const mine = ++revision
    controller?.abort()
    clearTimeout(timer)
    const draft = rule()
    count.value = null
    items.value = []
    error.value = ''
    loading.value = draft !== null
    if (!draft) return
    const snapshot = { ...draft }
    const request = new AbortController()
    controller = request
    try {
      const result = await engine.previewAlbum(snapshot, request.signal)
      if (mine !== revision) return
      count.value = result.count
      items.value = result.items
    } catch (e) {
      if (mine === revision && !isCancelled(e)) error.value = errorText(e)
    } finally {
      if (mine === revision) loading.value = false
    }
  }

  watch(
    rule,
    () => {
      revision++
      controller?.abort()
      clearTimeout(timer)
      count.value = null
      items.value = []
      error.value = ''
      loading.value = rule() !== null
      timer = setTimeout(() => void refresh(), 200)
    },
    { deep: true, immediate: true },
  )
  onScopeDispose(() => {
    revision++
    controller?.abort()
    clearTimeout(timer)
  })
  onScopeDispose(onBlocksChanged(refresh))
  return { count, items, loading, error, refresh }
}
