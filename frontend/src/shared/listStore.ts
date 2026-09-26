import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { Ref } from 'vue'

/**
 * A store holding one list from the engine (Things, People), loaded on
 * demand, with an index by id.
 */
export function defineListStore<T extends { id: number }>(id: string, fetch: () => Promise<T[]>) {
  return defineStore(id, () => {
    const list = ref([]) as Ref<T[]>
    const loaded = ref(false)
    const byId = computed(() => new Map(list.value.map((x) => [x.id, x])))

    let loading: Promise<void> | null = null
    /** (Re)load the list; concurrent calls share one request. */
    function load(): Promise<void> {
      loading ??= (async () => {
        try {
          list.value = await fetch()
          loaded.value = true
        } finally {
          loading = null
        }
      })()
      return loading
    }

    return { list, loaded, byId, load }
  })
}
