import { defineStore } from 'pinia'
import { computed, ref, shallowRef, triggerRef } from 'vue'
import {
  engine,
  EVENT_GAP_DEFAULT,
  Filter,
  onLibraryChanged,
  onProgress,
  startMedia,
  TAG_CONFIDENCE_DEFAULT,
} from '../bridge'
import type { Changes, DayBucket, GridItem, Progress, Settings, Source, View } from '../bridge'
import { errorText, logError, reportError } from '../shared/errors'
import { coalescedLoad } from '../shared/listStore'

const ZOOM_KEY = 'lumen.zoom'
export const ZOOM_MIN = 96
export const ZOOM_MAX = 320
const ZOOM_DEFAULT = 168

/** The single group search results live in (they have no days). */
export const RESULTS = 'results'

/** What the grid shows: part of the timeline, or search results (best first). */
export type Scope = { kind: 'timeline'; view: View } | { kind: 'search'; query: string }

export function scopeKey(s: Scope): string {
  return s.kind === 'search'
    ? `search:${s.query}`
    : `view:${s.view.filter}:${s.view.tag ?? 0}:${s.view.person ?? 0}:${s.view.album ?? 0}:${s.view.place ?? 0}:${s.view.event ?? 0}`
}

/** Whether a change in the library can change which days and assets a timeline view shows. */
export function affects(view: View, c: Changes): boolean {
  const album = !!view.album // a smart album can select by anything
  return c.assets || (c.tags && (!!view.tag || album)) || (c.people && (!!view.person || album))
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
  noAI: boolean // the description couldn't be searched
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
 * stale and they reload in place. While no grid is shown (attach/detach),
 * changes only mark the scope stale, and it reloads when shown again.
 */
export const useLibrary = defineStore('library', () => {
  const ready = ref(false)
  const sources = ref<Source[]>([])
  const settings = ref<Settings>({
    videos: false,
    aiOnBattery: false,
    tagConfidence: TAG_CONFIDENCE_DEFAULT,
    eventGapHours: EVENT_GAP_DEFAULT,
  })
  const progress = ref<Progress | null>(null)

  const scope = ref<Scope>({ kind: 'timeline', view: { filter: Filter.FilterAll } })
  const buckets = ref<DayBucket[]>([])
  const timelineLoaded = ref(false)
  const loadError = ref('')
  const searchInfo = ref<SearchInfo | null>(null)

  // day -> items. A shallowRef plus triggerRef keeps big maps cheap.
  const days = shallowRef(new Map<string, GridItem[]>())
  const fresh = new Set<string>() // days loaded since the last change
  const inflight = new Map<string, Promise<void>>() // day -> the load under way
  let generation = 0 // changes even when returning to a previously visited scope
  let loadRequest = 0
  let dayRevision = 0
  let shown = 0 // grids showing the scope (see attach)
  let stale = false // the library changed while no grid showed it

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

  /**
   * Load the initial state. Throws if the engine can't be reached. Events
   * are followed first, so none sent while this loads is missed.
   */
  async function init() {
    onProgress((p) => (progress.value = p))
    onLibraryChanged((c) => void refresh(c).catch(logError))
    const [s, set, p] = await Promise.all([engine.sources(), engine.settings(), engine.progress(), startMedia()])
    sources.value = s
    settings.value = set
    progress.value ??= p // an event is newer than the snapshot
    ready.value = true
  }

  /** Load the current scope's groups (and, for a search, its results). */
  async function load() {
    const s = scope.value
    const current = generation
    const request = ++loadRequest
    loadError.value = ''
    stale = false
    try {
      if (s.kind === 'search') {
        const r = await engine.search(s.query)
        if (current !== generation || request !== loadRequest) return
        searchInfo.value = { pending: r.pending, complete: r.complete, people: r.people, noAI: r.noAI }
        buckets.value = r.items.length ? [{ day: RESULTS, count: r.items.length }] : []
        days.value = new Map(r.items.length ? [[RESULTS, r.items]] : [])
        fresh.add(RESULTS)
      } else {
        const b = await engine.timeline(s.view)
        if (current !== generation || request !== loadRequest) return
        buckets.value = b
        const present = new Set(b.map((bucket) => bucket.day))
        days.value = new Map([...days.value].filter(([day]) => present.has(day)))
        if (selected.value && !present.has(selected.value.day)) selected.value = null
        if (viewer.value && !present.has(viewer.value.day)) viewer.value = null
      }
      timelineLoaded.value = true
    } catch (e) {
      if (current !== generation || request !== loadRequest) return
      loadError.value = errorText(e)
      throw e
    }
  }

  /**
   * Reload after the engine reports changes (all, if c is not given): counts,
   * then visible days on demand. Search results stay as they are
   * (reshuffling them while someone looks would be jarring); searching again
   * picks up new photos.
   */
  let refreshing: Promise<void> | null = null
  let refreshRevision = 0
  async function refresh(c?: Changes) {
    const s = scope.value
    if (s.kind === 'search' || (c && !affects(s.view, c) && !c.thumbs)) return
    if (!shown) {
      stale = true // loaded when a grid shows it again
      return
    }
    dayRevision++
    fresh.clear()
    if (c && !affects(s.view, c)) {
      // Only thumbnails changed: the days and counts stand, and the view
      // asks again for the days it shows.
      triggerRef(days)
      return
    }
    refreshRevision++
    if (refreshing) return refreshing
    refreshing = (async () => {
      try {
        for (;;) {
          const revision = refreshRevision
          await load()
          triggerRef(days) // the view re-requests what it shows
          if (revision === refreshRevision || isSearch.value) break
        }
      } finally {
        refreshing = null
      }
    })()
    return refreshing
  }

  /** Whether any of the days is neither loaded nor on its way. */
  function needsDays(want: string[]): boolean {
    return !isSearch.value && want.some((d) => !fresh.has(d) && !inflight.has(d))
  }

  /**
   * Make sure the given days are loaded (one request for the whole span),
   * waiting for any already on their way. Never throws: a failed load is
   * logged and retried when next needed.
   */
  async function ensureDays(want: string[]) {
    const s = scope.value
    if (s.kind === 'search') return // results arrive all at once
    const waiting = want.flatMap((d) => inflight.get(d) ?? [])
    const missing = want.filter((d) => !fresh.has(d) && !inflight.has(d)).sort()
    const [from, to] = [missing[0], missing.at(-1)]
    if (from !== undefined && to !== undefined) {
      const request: Promise<void> = loadDays(s.view, missing, from, to).finally(() => {
        for (const d of missing) if (inflight.get(d) === request) inflight.delete(d)
      })
      missing.forEach((d) => inflight.set(d, request))
      waiting.push(request)
    }
    await Promise.all(waiting)
  }

  async function loadDays(view: View, missing: string[], from: string, to: string) {
    const current = generation
    const revision = dayRevision
    try {
      const got = await engine.days(from, to, view)
      if (current !== generation || revision !== dayRevision) return
      const next = new Map(days.value)
      const inSpan = new Set(missing)
      for (const d of got) {
        if (!inSpan.has(d.day)) continue // another request owns intervening days
        next.set(d.day, d.items)
        fresh.add(d.day)
        inSpan.delete(d.day)
      }
      for (const d of inSpan) {
        next.delete(d) // the day emptied (e.g. files deleted)
        fresh.add(d)
      }
      days.value = next
      // A filter edit or source removal can remove the selected item while
      // leaving its day in place. Do not leave a panel with inactive actions.
      if (selected.value && missing.includes(selected.value.day) && !itemAt(selected.value)) selected.value = null
      if (viewer.value && missing.includes(viewer.value.day) && !itemAt(viewer.value)) viewer.value = null
    } catch (e) {
      logError(e)
    } finally {
      if (current === generation && revision !== dayRevision) triggerRef(days) // ask again for what is shown
    }
  }

  /**
   * The next (dir 1) or previous (dir -1) asset in grid order, loading the
   * neighbouring day if needed. A day reloaded meanwhile (the library
   * changed) is asked for again once.
   */
  async function neighbour(sel: Sel, dir: 1 | -1): Promise<Sel | null> {
    const list = days.value.get(sel.day) ?? []
    const i = list.findIndex((x) => x.id === sel.id)
    const near = i >= 0 ? list[i + dir] : undefined
    if (near) return { day: sel.day, id: near.id }
    const k = buckets.value.findIndex((b) => b.day === sel.day)
    const nb = buckets.value[k + dir]
    if (k < 0 || !nb) return null
    for (let tries = 0; tries < 2 && !days.value.has(nb.day); tries++) await ensureDays([nb.day])
    const next = days.value.get(nb.day)
    const edge = dir > 0 ? next?.[0] : next?.at(-1)
    return edge ? { day: nb.day, id: edge.id } : null
  }

  /** Show another scope. Searching again with the same words re-runs the search. */
  async function setScope(s: Scope) {
    if (scopeKey(s) === scopeKey(scope.value) && timelineLoaded.value && s.kind !== 'search') {
      if (stale) await refresh()
      return
    }
    scope.value = s
    generation++
    dayRevision++
    inflight.clear()
    selected.value = null
    viewer.value = null
    searchInfo.value = null
    days.value = new Map()
    buckets.value = []
    fresh.clear()
    timelineLoaded.value = false
    await load()
  }

  /**
   * A grid starts (attach) or stops (detach) showing the scope. The viewer
   * belongs to the grid, so it closes with it.
   */
  function attach() {
    shown++
  }
  function detach() {
    shown = Math.max(0, shown - 1)
    if (!shown) viewer.value = null
  }

  function setZoom(v: number) {
    zoom.value = v
    try {
      localStorage.setItem(ZOOM_KEY, String(v))
    } catch {
      /* not critical */
    }
  }

  /**
   * Flip an asset's favourite flag: shown at once, put back if the engine
   * refuses. Clicks on one asset take turns, so the flag always ends as the
   * engine has it.
   */
  const favouriting = new Map<number, Promise<void>>()
  async function toggleFavourite(p: Sel | null) {
    const it = itemAt(p)
    if (!p || !it) return
    const fav = !it.f
    const set = (value: boolean) => {
      const item = itemAt(p) // the day may have been reloaded meanwhile
      if (item) {
        item.f = value
        triggerRef(days)
      }
    }
    set(fav)
    const before = favouriting.get(it.id) ?? Promise.resolve()
    const saving = before.then(async () => {
      try {
        await engine.setFavourite(it.id, fav)
      } catch (e) {
        set(!fav)
        reportError(e)
        return
      }
      const s = scope.value
      if (s.kind === 'timeline' && (s.view.filter === Filter.FilterFavourites || !!s.view.album))
        await refresh().catch(logError)
    })
    favouriting.set(it.id, saving)
    await saving
    if (favouriting.get(it.id) === saving) favouriting.delete(it.id)
  }

  /**
   * Pause or resume indexing: shown at once, put back (and said) if the
   * engine refuses. The next progress event confirms it.
   */
  async function setPaused(paused: boolean) {
    const p = progress.value
    if (p) progress.value = { ...p, paused }
    try {
      await engine.setPaused(paused)
    } catch (e) {
      if (progress.value) progress.value = { ...progress.value, paused: !paused }
      reportError(e)
    }
  }

  /** Reload the folders (after adding or removing one), then what they hold. */
  const reloadSources = coalescedLoad(engine.sources, (got) => (sources.value = got))
  async function sourcesChanged() {
    await reloadSources()
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
    loadError,
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
    needsDays,
    ensureDays,
    neighbour,
    setScope,
    attach,
    detach,
    setZoom,
    toggleFavourite,
    setPaused,
    sourcesChanged,
  }
})
