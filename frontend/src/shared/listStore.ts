import { defineStore } from 'pinia'
import { computed, ref, shallowRef } from 'vue'

/**
 * A store holding one list from the engine (Things, People), loaded on
 * demand, with an index by id.
 */
export function defineListStore<T extends { id: number }>(id: string, fetch: () => Promise<T[]>) {
  return defineStore(id, () => {
    const list = shallowRef<T[]>([])
    const loaded = ref(false)
    const byId = computed(() => new Map(list.value.map((x) => [x.id, x])))

    let loading: Promise<void> | null = null
    let revision = 0
    /** Coalesce refreshes, including edits made during an in-flight read. */
    function load(): Promise<void> {
      revision++
      loading ??= (async () => {
        try {
          for (;;) {
            const requested = revision
            list.value = await fetch()
            loaded.value = true
            if (requested === revision) break
          }
        } finally {
          loading = null
        }
      })()
      return loading
    }

    return { list, loaded, byId, load }
  })
}
