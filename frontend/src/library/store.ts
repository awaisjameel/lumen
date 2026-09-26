import { defineStore } from 'pinia'
import { computed, ref, shallowRef, triggerRef } from 'vue'
import { engine, Filter, onLibraryChanged, onProgress } from '../bridge'
import type { DayBucket, GridItem, Progress, Settings, Source } from '../bridge'
import { logError } from '../shared/errors'

const ZOOM_KEY = 'lumen.zoom'
export const ZOOM_MIN = 96
export const ZOOM_MAX = 320
const ZOOM_DEFAULT = 168

/** A position in the library: which day, which asset. Survives items shifting. */
export interface Sel {
  day: string
  id: number
}

function loadZoom(): number {
  try {
    const v = Number(localStorage.getItem(ZOOM_KEY))
    if (v >= ZOOM_MIN && v <= ZOOM_MAX) return v
  } catch {
    /* default below */
  }
  return ZOOM_DEFAULT
}

/**
 * Library state: the day buckets for the current view, a cache of loaded
 * days, selection, and indexing progress. Days load lazily as they scroll
 * into view; a library change marks them stale and they reload in place.
 */
export const useLibrary = defineStore('library', () => {
  const ready = ref(false)
  const sources = ref<Source[]>([])
  const settings = ref<Settings>({ videos: false })
  const progress = ref<Progress | null>(null)

  const filter = ref<Filter>(Filter.FilterAll)
  const buckets = ref<DayBucket[]>([])
  const timelineLoaded = ref(false)

  // day -> items. A shallowRef plus triggerRef keeps big maps cheap.
  const days = shallowRef(new Map<string, GridItem[]>())
  const fresh = new Set<string>() // days loaded since the last change
  const inflight = new Set<string>()

  const selected = ref<Sel | null>(null)
  const viewer = ref<Sel | null>(null)
  const showDetails = ref(false)
  const zoom = ref(loadZoom())

  const total = computed(() => buckets.value.reduce((n, b) => n + b.count, 0))
  const selectedItem = computed(() => itemAt(selected.value))
  const viewerItem = computed(() => itemAt(viewer.value))

  function itemAt(p: Sel | null): GridItem | null {
    if (!p) return null
    return days.value.get(p.day)?.find((x) => x.id === p.id) ?? null
  }

  function indexOf(p: Sel): number {
    return days.value.get(p.day)?.findIndex((x) => x.id === p.id) ?? -1
  }

  /** The asset at index i of a day, as a selection (if that day is loaded). */
  function selAt(day: string, i: number): Sel | null {
    const it = days.value.get(day)?.[i]
    return it ? { day, id: it.id } : null
  }

  /** Load the initial state. Throws if the engine can't be reached. */
  async function init() {
    ;[sources.value, settings.value, progress.value] = await Promise.all([
      engine.sources(),
      engine.settings(),
      engine.progress(),
    ])
    ready.value = true
    onProgress((p) => (progress.value = p))
    onLibraryChanged(() => void refresh().catch(logError))
    if (sources.value.length) await loadTimeline()
  }

  async function loadTimeline() {
    buckets.value = await engine.timeline(filter.value)
    timelineLoaded.value = true
  }

  /** Reload after the engine reports changes: counts, then visible days on demand. */
  let refreshing: Promise<void> | null = null
  async function refresh() {
    if (refreshing) return refreshing
    refreshing = (async () => {
      try {
        await loadTimeline()
        fresh.clear()
        triggerRef(days) // the view re-requests what it shows
      } finally {
        refreshing = null
      }
    })()
    return refreshing
  }

  /**
   * Make sure the given days are loaded (one request for the whole span).
   * Never throws: a failed load is logged and retried when next needed.
   */
  async function ensureDays(want: string[]) {
    const missing = want.filter((d) => !fresh.has(d) && !inflight.has(d))
    if (!missing.length) return
    missing.sort()
    missing.forEach((d) => inflight.add(d))
    const f = filter.value
    try {
      const got = await engine.days(missing[0], missing[missing.length - 1], f)
      if (f !== filter.value) return // the view changed while loading
      const next = new Map(days.value)
      const inSpan = new Set(missing)
      for (const d of got) {
        next.set(d.day, d.items ?? [])
        fresh.add(d.day)
        inSpan.delete(d.day)
      }
      for (const d of inSpan) {
        next.delete(d) // the day emptied (e.g. files deleted)
        fresh.add(d)
      }
      days.value = next
    } catch (e) {
      logError(e)
    } finally {
      missing.forEach((d) => inflight.delete(d))
    }
  }

  /** The next (dir 1) or previous (dir -1) asset in timeline order, loading the neighbouring day if needed. */
  async function neighbour(sel: Sel, dir: 1 | -1): Promise<Sel | null> {
    const list = days.value.get(sel.day) ?? []
    const i = list.findIndex((x) => x.id === sel.id)
    if (i >= 0 && list[i + dir]) return { day: sel.day, id: list[i + dir].id }
    const k = buckets.value.findIndex((b) => b.day === sel.day)
    const nb = buckets.value[k + dir]
    if (k < 0 || !nb) return null
    await ensureDays([nb.day])
    const next = days.value.get(nb.day)
    if (!next?.length) return null
    return { day: nb.day, id: (dir > 0 ? next[0] : next[next.length - 1]).id }
  }

  async function setFilter(f: Filter) {
    if (f === filter.value && timelineLoaded.value) return
    filter.value = f
    selected.value = null
    viewer.value = null
    days.value = new Map()
    fresh.clear()
    timelineLoaded.value = false
    await loadTimeline()
  }

  function setZoom(v: number) {
    zoom.value = v
    try {
      localStorage.setItem(ZOOM_KEY, String(v))
    } catch {
      /* not critical */
    }
  }

  /** Flip an asset's favourite flag: shown at once, undone if the engine refuses. */
  async function toggleFavourite(p: Sel | null) {
    const it = itemAt(p)
    if (!it) return
    const fav = !it.f
    it.f = fav
    triggerRef(days)
    try {
      await engine.setFavourite(it.id, fav)
    } catch (e) {
      it.f = !fav
      triggerRef(days)
      logError(e)
      return
    }
    if (filter.value === Filter.FilterFavourites) await refresh().catch(logError)
  }

  async function reloadSources() {
    ;[sources.value, settings.value] = await Promise.all([engine.sources(), engine.settings()])
    await refresh()
  }

  return {
    ready,
    sources,
    settings,
    progress,
    filter,
    buckets,
    timelineLoaded,
    days,
    selected,
    viewer,
    showDetails,
    zoom,
    total,
    selectedItem,
    viewerItem,
    itemAt,
    indexOf,
    selAt,
    init,
    refresh,
    ensureDays,
    neighbour,
    setFilter,
    setZoom,
    toggleFavourite,
    reloadSources,
  }
})
