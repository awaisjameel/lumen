import { defineStore } from 'pinia'
import { computed, ref, shallowRef, triggerRef } from 'vue'
import { engine, Filter, onLibraryChanged, onProgress, TAG_CONFIDENCE_DEFAULT } from '../bridge'
import type { DayBucket, GridItem, Progress, Settings, Source, View } from '../bridge'
import { logError } from '../shared/errors'

const ZOOM_KEY = 'lumen.zoom'
export const ZOOM_MIN = 96
export const ZOOM_MAX = 320
const ZOOM_DEFAULT = 168

/** The single group search results live in (they have no days). */
export const RESULTS = 'results'

/** What the grid shows: part of the timeline, or search results (best first). */
export type Scope = { kind: 'timeline'; view: View } | { kind: 'search'; query: string }

export function scopeKey(s: Scope): string {
  return s.kind === 'search' ? `search:${s.query}` : `view:${s.view.filter}:${s.view.tag ?? 0}:${s.view.person ?? 0}`
}

/** A position in the library: which day, which asset. Survives items shifting. */
export interface Sel {
  day: string
  id: number
}

/** What a search found beyond its items. */
export interface SearchInfo {
  pending: number // photos the AI hasn't seen yet
  complete: boolean
  people: string[] // people named in the query
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
 * Library state: the groups (days, or one group of search results) for the
 * current scope, a cache of loaded items, selection, and indexing progress.
 * Days load lazily as they scroll into view; a library change marks them
 * stale and they reload in place.
 */
export const useLibrary = defineStore('library', () => {
  const ready = ref(false)
  const sources = ref<Source[]>([])
  const settings = ref<Settings>({ videos: false, aiOnBattery: false, tagConfidence: TAG_CONFIDENCE_DEFAULT })
  const progress = ref<Progress | null>(null)

  const scope = ref<Scope>({ kind: 'timeline', view: { filter: Filter.FilterAll } })
  const buckets = ref<DayBucket[]>([])
  const timelineLoaded = ref(false)
  const searchInfo = ref<SearchInfo | null>(null)

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
  const isSearch = computed(() => scope.value.kind === 'search')

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
    if (sources.value.length) await load()
  }

  /** Load the current scope's groups (and, for a search, its results). */
  async function load() {
    const s = scope.value
    if (s.kind === 'search') {
      const r = await engine.search(s.query)
      if (scopeKey(s) !== scopeKey(scope.value)) return // superseded while searching
      searchInfo.value = { pending: r.pending, complete: r.complete, people: r.people }
      buckets.value = r.items.length ? [{ day: RESULTS, count: r.items.length }] : []
      days.value = new Map(r.items.length ? [[RESULTS, r.items]] : [])
      fresh.add(RESULTS)
    } else {
      const b = await engine.timeline(s.view)
      if (scopeKey(s) !== scopeKey(scope.value)) return
      buckets.value = b
    }
    timelineLoaded.value = true
  }

  /**
   * Reload after the engine reports changes: counts, then visible days on
   * demand. Search results stay as they are (reshuffling them while someone
   * looks would be jarring); searching again picks up new photos.
   */
  let refreshing: Promise<void> | null = null
  async function refresh() {
    if (isSearch.value) return
    if (refreshing) return refreshing
    refreshing = (async () => {
      try {
        await load()
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
    const s = scope.value
    if (s.kind === 'search') return // results arrive all at once
    const missing = want.filter((d) => !fresh.has(d) && !inflight.has(d))
    if (!missing.length) return
    missing.sort()
    missing.forEach((d) => inflight.add(d))
    try {
      const got = await engine.days(missing[0], missing[missing.length - 1], s.view)
      if (scopeKey(s) !== scopeKey(scope.value)) return // the view changed while loading
      const next = new Map(days.value)
      const inSpan = new Set(missing)
      for (const d of got) {
        next.set(d.day, d.items)
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

  /** The next (dir 1) or previous (dir -1) asset in grid order, loading the neighbouring day if needed. */
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

  /** Show another scope. Searching again with the same words re-runs the search. */
  async function setScope(s: Scope) {
    if (scopeKey(s) === scopeKey(scope.value) && timelineLoaded.value && s.kind !== 'search') return
    scope.value = s
    selected.value = null
    viewer.value = null
    searchInfo.value = null
    days.value = new Map()
    buckets.value = []
    fresh.clear()
    timelineLoaded.value = false
    await load()
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
    const s = scope.value
    if (s.kind === 'timeline' && s.view.filter === Filter.FilterFavourites) await refresh().catch(logError)
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
    scope,
    buckets,
    timelineLoaded,
    searchInfo,
    days,
    selected,
    viewer,
    showDetails,
    zoom,
    total,
    selectedItem,
    viewerItem,
    isSearch,
    itemAt,
    indexOf,
    selAt,
    init,
    refresh,
    ensureDays,
    neighbour,
    setScope,
    setZoom,
    toggleFavourite,
    reloadSources,
  }
})
