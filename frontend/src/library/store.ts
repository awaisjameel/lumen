import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref, shallowRef } from 'vue'
import {
  engine,
  EVENT_GAP_DEFAULT,
  THUMB_CACHE_DEFAULT_MB,
  Filter,
  isCancelled,
  onLibraryChanged,
  onProgress,
  startMedia,
  TAG_CONFIDENCE_DEFAULT,
} from '../bridge'
import type { Changes, DayBucket, GridItem, Progress, SearchResults, Settings, Source, View } from '../bridge'
import { onBlocksChanged } from '../shared/block'
import { errorText, logError, reportError } from '../shared/errors'
import { coalescedLoad } from '../shared/listStore'
import { latestSave } from '../shared/latestSave'
import type { LatestSave } from '../shared/latestSave'
import { showTrashUndo } from '../trash/undo'

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
type SearchInfo = Omit<SearchResults, 'items'>

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
    thumbCacheMB: THUMB_CACHE_DEFAULT_MB,
    ocr: false,
    xmp: false,
  })
  const settingsError = ref('')
  let eventGapSave: LatestSave<number> | null = null
  let confidenceSave: LatestSave<number> | null = null
  let videosSave: LatestSave<boolean> | null = null
  let batterySave: LatestSave<boolean> | null = null
  let thumbCacheSave: LatestSave<number> | null = null
  let ocrSave: LatestSave<boolean> | null = null
  const settingFailed = (e: unknown) => (settingsError.value = errorText(e))

  function saveVideos(on: boolean) {
    videosSave ??= latestSave(engine.setVideos, settings.value.videos, {
      rollback: (value) => (settings.value.videos = value),
      failed: settingFailed,
    })
    settings.value.videos = on
    settingsError.value = ''
    return videosSave(on)
  }

  function saveAIOnBattery(on: boolean) {
    batterySave ??= latestSave(engine.setAIOnBattery, settings.value.aiOnBattery, {
      rollback: (value) => (settings.value.aiOnBattery = value),
      failed: settingFailed,
    })
    settings.value.aiOnBattery = on
    settingsError.value = ''
    return batterySave(on)
  }

  function saveOCR(on: boolean) {
    ocrSave ??= latestSave(engine.setOCR, settings.value.ocr, {
      rollback: (value) => (settings.value.ocr = value),
      failed: settingFailed,
    })
    settings.value.ocr = on
    settingsError.value = ''
    return ocrSave(on)
  }

  function saveEventGap(hours: number) {
    eventGapSave ??= latestSave(engine.setEventGapHours, settings.value.eventGapHours, {
      saved: () => refresh(),
      rollback: (value) => (settings.value.eventGapHours = value),
      failed: settingFailed,
    })
    settings.value.eventGapHours = hours
    settingsError.value = ''
    return eventGapSave(hours)
  }

  function saveThumbCacheMB(mb: number) {
    thumbCacheSave ??= latestSave(engine.setThumbCacheMB, settings.value.thumbCacheMB, {
      rollback: (value) => (settings.value.thumbCacheMB = value),
      failed: settingFailed,
    })
    settings.value.thumbCacheMB = mb
    settingsError.value = ''
    return thumbCacheSave(mb)
  }

  function confidenceQueue() {
    confidenceSave ??= latestSave(engine.setTagConfidence, settings.value.tagConfidence, {
      saved: () => refresh(),
      rollback: (value) => (settings.value.tagConfidence = value),
      failed: settingFailed,
    })
    return confidenceSave
  }

  function intendTagConfidence(value: number) {
    confidenceQueue().touch()
    settings.value.tagConfidence = value
    settingsError.value = ''
  }

  function saveTagConfidence(value: number) {
    return confidenceQueue()(value)
  }
  const progress = shallowRef<Progress | null>(null) // replaced whole by each event

  const scope = ref<Scope>({ kind: 'timeline', view: { filter: Filter.FilterAll } })
  const buckets = shallowRef<DayBucket[]>([]) // replaced whole: thousands of days
  const timelineLoaded = ref(false)
  const loadError = ref('')
  const searchInfo = ref<SearchInfo | null>(null)

  // day -> items. A shallowRef keeps big maps cheap; notifyDays tells
  // everything shown from it that it changed.
  const days = shallowRef(new Map<string, GridItem[]>())
  /**
   * Tells the views days changed (or its items did, in place). A new Map, not
   * triggerRef: a watcher of () => days skips its callback when the value is
   * the same object, and the grid's (LibraryView) is how stale days are asked
   * for again.
   */
  function notifyDays() {
    days.value = new Map(days.value)
  }
  const fresh = new Set<string>() // days loaded since the last change
  const inflight = new Map<string, Promise<void>>() // day -> the load under way
  let generation = 0 // changes even when returning to a previously visited scope
  let loadRequest = 0
  let dayRevision = 0
  // Cancel reads whose answers would be dropped: the scope's groups when it
  // loads again, the days' when they go stale (dayRevision).
  let loadAbort = new AbortController()
  let daysAbort = new AbortController()
  let visibilityAbort = new AbortController()
  let visibilityRequest = 0
  let visibilityRevision = 0
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
  const inTrash = computed(() => scope.value.kind === 'timeline' && scope.value.view.filter === Filter.FilterTrash)
  const trashCount = ref(0) // for the sidebar

  function itemAt(p: Sel | null): GridItem | null {
    if (!p) return null
    return days.value.get(p.day)?.find((x) => x.id === p.id) ?? null
  }

  function indexOf(p: Sel): number {
    return days.value.get(p.day)?.findIndex((x) => x.id === p.id) ?? -1
  }

  // Assets being taken out of the view (trashed, restored, deleted) -> where
  // the viewer and selection go once they are gone. A reload that lands before
  // the engine answers drops the asset from the grid; it must advance, not close.
  const advancing = new Map<number, Sel | null>()
  function movedOn(p: Sel): Sel | null {
    return advancing.get(p.id) ?? null
  }

  /** The asset at index i of a day, as a selection (if that day is loaded). */
  function selAt(day: string, i: number): Sel | null {
    const it = days.value.get(day)?.[i]
    return it ? { day, id: it.id } : null
  }

  let stopProgress: (() => void) | null = null
  let stopChanges: (() => void) | null = null
  let disposed = false
  const initialise = coalescedLoad(
    async () => {
      const [s, set, p] = await Promise.all([engine.sources(), engine.settings(), engine.progress(), startMedia()])
      return { s, set, p }
    },
    ({ s, set, p }) => {
      if (disposed) return
      sources.value = s
      settings.value = set
      progress.value ??= p // an event is newer than the snapshot
      ready.value = true
    },
  )
  /**
   * Load the initial state. Throws if the engine can't be reached. Events
   * are followed first, so none sent while this loads is missed.
   */
  async function init() {
    if (ready.value || disposed) return
    stopProgress ??= onProgress((p) => (progress.value = p))
    stopChanges ??= onLibraryChanged((c) => {
      void refresh(c).catch(logError)
      if (c.assets) void loadTrashCount()
    })
    await initialise()
    void loadTrashCount()
  }
  const stopBlocks = onBlocksChanged(() => refreshVisibility())
  onScopeDispose(() => {
    stopBlocks()
    disposed = true
    stopProgress?.()
    stopChanges?.()
    generation++
    loadRequest++
    visibilityRequest++
    loadAbort.abort()
    daysAbort.abort()
    visibilityAbort.abort()
  })

  /** Load the current scope's groups (and, for a search, its results). */
  async function load() {
    const s = scope.value
    const current = generation
    const request = ++loadRequest
    let visibility = visibilityRevision
    visibilityAbort.abort()
    visibilityRequest++
    loadAbort.abort()
    loadAbort = new AbortController()
    const { signal } = loadAbort
    loadError.value = ''
    stale = false
    try {
      if (s.kind === 'search') {
        const r = await engine.search(s.query, signal)
        if (current !== generation || request !== loadRequest) return
        let items = r.items
        // A pre-change answer must pass visibility before any of its hits
        // reach the grid. Repeat if another change arrived during the check.
        while (visibility !== visibilityRevision) {
          visibility = visibilityRevision
          items = await visibleItems(items, signal)
          if (current !== generation || request !== loadRequest) return
        }
        // A prune of the previous results must not replace these verified hits.
        visibilityAbort.abort()
        visibilityRequest++
        searchInfo.value = {
          pending: r.pending,
          complete: r.complete,
          people: r.people,
          noAI: r.noAI,
          blocked: r.blocked,
        }
        buckets.value = items.length ? [{ day: RESULTS, count: items.length }] : []
        days.value = new Map(items.length ? [[RESULTS, items]] : [])
        fresh.add(RESULTS)
      } else {
        const b = await engine.timeline(s.view, signal)
        if (current !== generation || request !== loadRequest) return
        buckets.value = b
        const present = new Set(b.map((bucket) => bucket.day))
        days.value = new Map([...days.value].filter(([day]) => present.has(day)))
        if (selected.value && !present.has(selected.value.day)) selected.value = movedOn(selected.value)
        if (viewer.value && !present.has(viewer.value.day)) viewer.value = movedOn(viewer.value)
      }
      timelineLoaded.value = true
    } catch (e) {
      if (current !== generation || request !== loadRequest || isCancelled(e)) return
      loadError.value = errorText(e)
      throw e
    }
  }

  /** Every loaded day is stale: reads of them under way are cancelled. */
  function staleDays() {
    dayRevision++
    daysAbort.abort()
    daysAbort = new AbortController()
  }

  async function visibleItems(items: GridItem[], signal: AbortSignal): Promise<GridItem[]> {
    const visible = new Set(
      await engine.visibleAssets(
        items.map((item) => item.id),
        signal,
      ),
    )
    return items.filter((item) => visible.has(item.id))
  }

  async function pruneSearch() {
    const current = generation
    const load = loadRequest
    const request = ++visibilityRequest
    visibilityAbort.abort()
    visibilityAbort = new AbortController()
    const items = days.value.get(RESULTS) ?? []
    try {
      const kept = await visibleItems(items, visibilityAbort.signal)
      if (current !== generation || load !== loadRequest || request !== visibilityRequest) return
      buckets.value = kept.length ? [{ day: RESULTS, count: kept.length }] : []
      days.value = new Map(kept.length ? [[RESULTS, kept]] : [])
      if (selected.value && !itemAt(selected.value)) selected.value = movedOn(selected.value)
      if (viewer.value && !itemAt(viewer.value)) viewer.value = movedOn(viewer.value)
      stale = false
    } catch (e) {
      if (current !== generation || request !== visibilityRequest || isCancelled(e)) return
      throw e
    }
  }

  /**
   * Reload after the engine reports changes (all, if c is not given): counts,
   * then visible days on demand. Search results only lose hidden hits;
   * searching again picks up new photos without reshuffling a live search.
   */
  let refreshing: Promise<void> | null = null
  let refreshRevision = 0
  async function refresh(c?: Changes) {
    const s = scope.value
    if (s.kind === 'search') {
      if (c && !c.assets) return
      visibilityRevision++
      if (!shown) {
        visibilityAbort.abort()
        visibilityRequest++
        stale = true
        return
      }
      return pruneSearch()
    }
    if (c && !affects(s.view, c) && !c.thumbs) return
    if (!shown) {
      stale = true // loaded when a grid shows it again
      return
    }
    staleDays()
    fresh.clear()
    if (c && !affects(s.view, c)) {
      // Only thumbnails changed: the days and counts stand, and the view
      // asks again for the days it shows.
      notifyDays()
      return
    }
    refreshRevision++
    if (refreshing) return refreshing
    refreshing = (async () => {
      try {
        for (;;) {
          const revision = refreshRevision
          try {
            await load()
          } catch (e) {
            if (revision === refreshRevision || isSearch.value || disposed) throw e
            // A failed older read must not discard a newer library change.
            continue
          }
          notifyDays() // the view re-requests what it shows
          if (revision === refreshRevision || isSearch.value || disposed) break
        }
      } finally {
        refreshing = null
      }
    })()
    return refreshing
  }

  /** A privacy change must remove covered search hits immediately. */
  async function refreshVisibility() {
    if (scope.value.kind === 'search') await setScope(scope.value)
    else {
      generation++
      staleDays()
      inflight.clear()
      fresh.clear()
      days.value = new Map()
      selected.value = null
      viewer.value = null
      await refresh({ assets: true, thumbs: false, tags: true, people: true })
    }
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
      const current = generation
      const revision = dayRevision
      const request: Promise<void> = loadDays(s.view, missing, from, to).finally(() => {
        for (const d of missing) if (inflight.get(d) === request) inflight.delete(d)
        // Released before the grid hears of it, so it asks for these days again.
        if (current === generation && revision !== dayRevision) notifyDays()
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
      const got = await engine.days(from, to, view, daysAbort.signal)
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
      if (selected.value && missing.includes(selected.value.day) && !itemAt(selected.value))
        selected.value = movedOn(selected.value)
      if (viewer.value && missing.includes(viewer.value.day) && !itemAt(viewer.value))
        viewer.value = movedOn(viewer.value)
    } catch (e) {
      if (!isCancelled(e)) logError(e)
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
    staleDays()
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
  const favouriting = new Map<number, LatestSave<boolean>>()
  async function toggleFavourite(p: Sel | null) {
    const it = itemAt(p)
    if (!p || !it) return
    const fav = !it.f
    const set = (value: boolean) => {
      const item = itemAt(p) // the day may have been reloaded meanwhile
      if (item) {
        item.f = value
        notifyDays()
      }
    }
    let save = favouriting.get(it.id)
    if (!save) {
      save = latestSave((value) => engine.setFavourite(it.id, value), it.f ?? false, {
        rollback: set,
        failed: reportError,
        saved: async () => {
          const s = scope.value
          if (s.kind === 'timeline' && (s.view.filter === Filter.FilterFavourites || !!s.view.album))
            await refresh().catch(logError)
        },
      })
      favouriting.set(it.id, save)
    }
    set(fav)
    const saving = save(fav)
    await saving
    if (favouriting.get(it.id) === save) favouriting.delete(it.id)
  }

  /**
   * Pause or resume indexing: shown at once, put back (and said) if the
   * engine refuses. The next progress event confirms it.
   */
  let pauseSave: LatestSave<boolean> | null = null
  function setPaused(paused: boolean) {
    const p = progress.value
    pauseSave ??= latestSave(engine.setPaused, p?.paused ?? false, {
      rollback: (value) => {
        if (progress.value) progress.value = { ...progress.value, paused: value }
      },
      failed: reportError,
    })
    if (p) progress.value = { ...p, paused }
    return pauseSave(paused)
  }

  const readTrashCount = coalescedLoad(engine.trashCount, (n) => (trashCount.value = n))
  /** Read how many assets the Trash holds; a failure is logged, the count stays. */
  async function loadTrashCount() {
    try {
      await readTrashCount()
    } catch (e) {
      logError(e)
    }
  }

  /** The Trash changed: the grid and the Trash's count show it now. */
  async function trashChanged() {
    await Promise.all([refresh().catch(logError), loadTrashCount()])
  }

  /**
   * Run a change that takes the asset at p out of this view (trashing,
   * restoring, deleting from disk) and move the viewer and the selection on
   * to its neighbour, the next photo or else the previous. Failures are
   * reported; the result says whether the change was made, with its value.
   */
  async function removeFromView<T>(p: Sel, change: () => Promise<T>): Promise<{ value: T } | null> {
    const next = (await neighbour(p, 1)) ?? (await neighbour(p, -1))
    let done: { value: T } | null = null
    advancing.set(p.id, next)
    try {
      done = { value: await change() }
    } catch (e) {
      reportError(e)
    } finally {
      advancing.delete(p.id)
    }
    if (done) {
      if (viewer.value?.id === p.id) viewer.value = next
      if (selected.value?.id === p.id) selected.value = next
    }
    await trashChanged() // even after a failure: it may have done part of the work
    return done
  }

  /** Move an asset to the Trash, which offers to undo it. */
  async function moveToTrash(p: Sel) {
    if (await removeFromView(p, () => engine.trashAssets([p.id]))) showTrashUndo([p.id])
  }

  /** Take the trashed asset at p out of the Trash. */
  async function restoreFromTrash(p: Sel) {
    await removeFromView(p, () => engine.restoreAssets([p.id]))
  }

  /** Take assets out of the Trash (Undo of moving them there). Throws if the engine refuses. */
  async function restoreAssets(ids: number[]) {
    try {
      await engine.restoreAssets(ids)
    } finally {
      await trashChanged()
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
    settingsError,
    saveVideos,
    saveAIOnBattery,
    saveOCR,
    saveEventGap,
    saveThumbCacheMB,
    intendTagConfidence,
    saveTagConfidence,
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
    inTrash,
    trashCount,
    itemAt,
    indexOf,
    selAt,
    init,
    refresh,
    refreshVisibility,
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
    loadTrashCount,
    trashChanged,
    removeFromView,
    moveToTrash,
    restoreFromTrash,
    restoreAssets,
  }
})
