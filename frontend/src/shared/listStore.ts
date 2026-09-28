import { defineStore } from 'pinia'
import { computed, ref, shallowRef } from 'vue'

/**
 * Wraps a read of the engine so at most one runs at a time. Calls made while
 * one runs queue a single follow-up read (edits made during it must show), and
 * only the newest read's result is applied.
 */
export function coalescedLoad<T>(fetch: () => Promise<T>, apply: (value: T) => void): () => Promise<void> {
  let loading: Promise<void> | null = null
  let revision = 0
  return () => {
    revision++
    loading ??= (async () => {
      try {
        for (;;) {
          const requested = revision
          try {
            const got = await fetch()
            if (requested !== revision) continue
            apply(got)
            return
          } catch (e) {
            if (requested === revision) throw e
            // An older failed read must not discard a queued refresh.
          }
        }
      } finally {
        loading = null
      }
    })()
    return loading
  }
}

/**
 * A store holding one list from the engine (Things, People, Albums), loaded on
 * demand, with an index by id.
 */
export function defineListStore<T extends { id: number }>(id: string, fetch: () => Promise<T[]>) {
  return defineStore(id, () => {
    const list = shallowRef<T[]>([])
    const loaded = ref(false)
    const byId = computed(() => new Map(list.value.map((x) => [x.id, x])))
    const load = coalescedLoad(fetch, (got) => {
      list.value = got
      loaded.value = true
    })
    return { list, loaded, byId, load }
  })
}
