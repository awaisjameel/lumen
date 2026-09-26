import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { engine } from '../bridge'
import type { Tag } from '../bridge'

/** Things (tags) with their counts and covers, loaded on demand. */
export const useThings = defineStore('things', () => {
  const list = ref<Tag[]>([])
  const loaded = ref(false)

  const byId = computed(() => new Map(list.value.map((t) => [t.id, t])))

  let loading: Promise<void> | null = null
  /** (Re)load the list; concurrent calls share one request. */
  function load(): Promise<void> {
    loading ??= (async () => {
      try {
        list.value = await engine.things()
        loaded.value = true
      } finally {
        loading = null
      }
    })()
    return loading
  }

  return { list, loaded, byId, load }
})
