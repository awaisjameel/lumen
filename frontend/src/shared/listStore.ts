import { defineStore } from 'pinia'
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { errorText, logError } from './errors'

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

function listState<T>(fetch: () => Promise<T[]>, active: () => boolean = () => true) {
  const list = shallowRef<T[]>([])
  const loaded = ref(false)
  const error = ref('')
  const read = coalescedLoad(
    () => (active() ? fetch() : Promise.resolve([])),
    (got) => {
      if (!active()) return
      list.value = got
      loaded.value = true
      error.value = ''
    },
  )
  async function load() {
    if (!active()) return
    try {
      await read()
    } catch (e) {
      if (!active()) return
      error.value = errorText(e)
      throw e
    }
  }
  return { list, loaded, error, load }
}

export function useList<T>(fetch: () => Promise<T[]>) {
  let active = true
  const state = listState(fetch, () => active)
  onMounted(() => void state.load().catch(logError))
  onBeforeUnmount(() => {
    active = false
  })
  return state
}

/**
 * A store holding one list from the engine (Things, People, Albums), loaded on
 * demand, with an index by id. error says why the last load failed ("" once
 * one succeeds); load also rejects, for callers that wait on it.
 */
export function defineListStore<T extends { id: number }>(id: string, fetch: () => Promise<T[]>) {
  return defineStore(id, () => {
    const state = listState(fetch)
    const byId = computed(() => new Map(state.list.value.map((item) => [item.id, item])))
    return { ...state, byId }
  })
}
