import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref, shallowRef } from 'vue'
import {
  engine,
  ErrorCode,
  errorCode,
  EVENT_GAP_DEFAULT,
  THUMB_CACHE_DEFAULT_MB,
  Filter,
  isCancelled,
  onLibraryChanged,
  onProgress,
  PrefCategory,
  startMedia,
  TAG_CONFIDENCE_DEFAULT,
} from '../bridge'
import type {
  Changes,
  DayBucket,
  Family,
  GridItem,
  Progress,
  SearchResults,
  Selection,
  Settings,
  Source,
  View,
} from '../bridge'
import { onBlocksChanged } from '../shared/block'
import { errorText, logError, reportError } from '../shared/errors'
import { coalescedLoad } from '../shared/listStore'
import { latestSave } from '../shared/latestSave'
import type { LatestSave } from '../shared/latestSave'
import { deleteFromDisk } from '../trash/actions'
import { showTrashUndo } from '../trash/undo'
import { usePreferences } from '../shared/preferences'

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
    // The machine's threads come with the settings; none until they load.
    cpuThreads: 0,
    maxCPUThreads: 0,
    defaultCPUThreads: 0,
    pauseFullScreen: true,
    pauseBusy: true,
    idleOnly: false,
    excludedFormats: [],
  })
  /** Why a page action in Settings failed (adding a folder, exporting sidecars). */
  const settingsError = ref('')
  /** Why the newest save of a setting failed, by setting; cleared by its next change. */
  const settingErrors = ref<Partial<Record<keyof Settings, string>>>({})
  let confidenceSave: LatestSave<number> | null = null
  const settingFailed = (key: keyof Settings) => (e: unknown) => (settingErrors.value[key] = errorText(e))
  // Each setting's save queue remembers the last value saved, to roll back
  // to: reloadSettings forgets them all once the library holds other values.
  const settingQueues: { forget: () => void; flush: () => Promise<void> }[] = []
  let resetting: Promise<void> | null = null

  /**
   * A save for one setting: it shows the new value at once, saves the newest
   * one asked for (see latestSave), and on failure puts back the last saved
   * and says why. saved runs after a save that is still the newest.
   */
  function settingSave<K extends keyof Settings>(
    key: K,
    save: (value: Settings[K]) => Promise<void>,
    saved?: () => unknown,
  ): (value: Settings[K]) => Promise<void> {
    let queue: LatestSave<Settings[K]> | null = null
    settingQueues.push({ forget: () => (queue = null), flush: () => queue?.flush() ?? Promise.resolve() })
    return async (value) => {
      while (resetting) await resetting
      queue ??= latestSave(save, settings.value[key], {
        saved,
        rollback: (last) => (settings.value[key] = last),
        failed: settingFailed(key),
      })
      settings.value[key] = value
      settingErrors.value[key] = ''
      return queue(value)
    }
  }

  const saveVideos = settingSave('videos', engine.setVideos)
  const saveAIOnBattery = settingSave('aiOnBattery', engine.setAIOnBattery)
  const savePauseFullScreen = settingSave('pauseFullScreen', engine.setPauseFullScreen)
  const savePauseBusy = settingSave('pauseBusy', engine.setPauseBusy)
  const saveIdleOnly = settingSave('idleOnly', engine.setIdleOnly)
  const saveOCR = settingSave('ocr', engine.setOCR)
  const saveXMP = settingSave('xmp', engine.setXMP)
  const saveEventGap = settingSave('eventGapHours', engine.setEventGapHours, () => refresh())
  const saveThumbCacheMB = settingSave('thumbCacheMB', engine.setThumbCacheMB)
  const saveCPUThreads = settingSave('cpuThreads', engine.setCPUThreads)

  function confidenceQueue() {
    confidenceSave ??= latestSave(engine.setTagConfidence, settings.value.tagConfidence, {
      saved: () => refresh(),
      rollback: (value) => (settings.value.tagConfidence = value),
      failed: settingFailed('tagConfidence'),
    })
    return confidenceSave
  }

  function intendTagConfidence(value: number) {
    confidenceQueue().touch()
    settings.value.tagConfidence = value
    settingErrors.value.tagConfidence = ''
  }

  async function saveTagConfidence(value: number) {
    while (resetting) await resetting
    return confidenceQueue()(value)
  }

  // Each format's exclusion is saved in turn, so two changed quickly end as
  // the user left them; a failure shows what the library holds again.
  let formatSaves = Promise.resolve()
  let storedFormats: Family[] | null = null
  let formatRevision = 0
  /** Makes indexing leave a photo format out, or take it in again. */
  async function saveFormatExcluded(family: Family, excluded: boolean): Promise<void> {
    while (resetting) await resetting
    storedFormats ??= [...settings.value.excludedFormats]
    const revision = ++formatRevision
    settingErrors.value.excludedFormats = ''
    const others = settings.value.excludedFormats.filter((f) => f !== family)
    settings.value.excludedFormats = excluded ? [...others, family] : others
    formatSaves = formatSaves.then(async () => {
      try {
        await engine.setFormatExcluded(family, excluded)
        const others = (storedFormats ?? []).filter((f) => f !== family)
        storedFormats = excluded ? [...others, family] : others
        if (revision === formatRevision) {
          settings.value.excludedFormats = [...storedFormats]
          settingErrors.value.excludedFormats = ''
        }
      } catch (e) {
        if (revision === formatRevision) {
          settingFailed('excludedFormats')(e)
          settings.value.excludedFormats = [...(storedFormats ?? [])]
        }
      }
    })
    return formatSaves
  }

  /** Reads the settings again as the library holds them (after a reset changed them there). */
  async function reloadSettings(): Promise<void> {
    const set = await engine.settings()
    for (const queue of settingQueues) queue.forget()
    confidenceSave = null
    storedFormats = null
    settingErrors.value = {}
    settings.value = set
    await refresh() // the tag confidence and the event gap decide what the views hold
  }

  /** A reset follows pending saves; later edits wait for the reset and its refreshed values. */
  function resetPreferences(categories: PrefCategory[]): Promise<void> {
    const previous = resetting
    const task = (async () => {
      if (previous) await previous
      await Promise.all([...settingQueues.map((queue) => queue.flush()), confidenceSave?.flush(), formatSaves])
      try {
        await usePreferences().reset(categories)
      } finally {
        if (
          categories.some((category) =>
            [PrefCategory.PrefIndexing, PrefCategory.PrefSearch, PrefCategory.PrefFiles].includes(category),
          )
        )
          await reloadSettings()
      }
    })()
    // The initiating view reports failure; queued edits only need its completion.
    const current = task
      .catch(() => undefined)
      .finally(() => {
        if (resetting === current) resetting = null
      })
    resetting = current
    return task
  }
  const progress = shallowRef<Progress | null>(null) // replaced whole by each event

  const scope = ref<Scope>({ kind: 'timeline', view: { filter: Filter.FilterAll } })
  const buckets = shallowRef<DayBucket[]>([]) // replaced whole: thousands of days
  const timelineLoaded = ref(false)
  const loadError = ref('')
  /** Why the newest load of days failed: their cells wait until retryDays, a scroll or a library change. */
  const daysError = ref('')
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
  let resultsAbort = new AbortController()
  let resultsRequest = 0
  let resultsRevision = 0 // search hits may have changed (hidden, or a new thumbnail)
  let shown = 0 // grids showing the scope (see attach)
  let stale = false // the library changed while no grid showed it

  const selected = ref<Sel | null>(null)
  const selectedIDs = shallowRef(new Set<number>())
  const selectionSnapshot = shallowRef<Selection | null>(null)
  let selectionRequest = 0
  let selectionAnchor: Sel | null = null

  // Every id of the timeline scope, as last read. A Shift range needs them all
  // to know what lies between two photos; the next step asks the engine only
  // whether membership has changed since, not for a whole library again.
  let timelineIDs: { scope: Scope; result: Selection } | null = null

  async function resultSelection() {
    const current = scope.value
    if (current.kind === 'search') {
      const ids = (days.value.get(RESULTS) ?? []).map((item) => item.id)
      return engine.selection({ filter: Filter.FilterAll }, ids, true)
    }
    const kept = timelineIDs?.scope === current ? timelineIDs.result : null
    if (kept) {
      try {
        await engine.validateSelection(kept)
        return kept
      } catch (e) {
        if (errorCode(e) !== ErrorCode.SelectionChanged) throw e
      }
    }
    const result = await engine.selection(current.view, [], false)
    timelineIDs = { scope: current, result }
    return result
  }

  async function selectItems(sel: Sel, control: boolean, shift: boolean) {
    const mine = ++selectionRequest
    const anchor = selectionAnchor ?? selected.value
    selected.value = sel
    if (shift && anchor) {
      try {
        const result = await resultSelection()
        if (mine !== selectionRequest) return
        const ids = result.ids
        const from = ids.indexOf(anchor.id),
          to = ids.indexOf(sel.id)
        if (from < 0 || to < 0) return
        const range = ids.slice(Math.min(from, to), Math.max(from, to) + 1)
        const present = new Set(ids)
        selectedIDs.value = new Set(
          control ? [...selectedIDs.value].filter((id) => present.has(id)).concat(range) : range,
        )
        selectionSnapshot.value = { ...result, ids: [...selectedIDs.value] }
      } catch (e) {
        if (mine === selectionRequest) reportError(e)
      }
    } else {
      selectionAnchor = sel
      const ids = control ? new Set(selectedIDs.value) : new Set<number>()
      if (control && ids.has(sel.id)) ids.delete(sel.id)
      else ids.add(sel.id)
      selectedIDs.value = ids
      selectionSnapshot.value = null
      if (!ids.size) selected.value = null
    }
  }

  async function selectAllResults() {
    const mine = ++selectionRequest
    try {
      const result = await resultSelection()
      if (mine !== selectionRequest) return
      selectedIDs.value = new Set(result.ids)
      selectionSnapshot.value = result
    } catch (e) {
      if (mine === selectionRequest) reportError(e)
    }
  }

  function clearSelection() {
    selectionAnchor = null
    selectionRequest++
    selectedIDs.value = new Set()
    selectionSnapshot.value = null
  }

  function selectVisible(ids: number[]) {
    clearSelection()
    selectedIDs.value = new Set(ids)
  }
  const viewer = ref<Sel | null>(null)
  const showDetails = ref(false)

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
    clearSelection()
    timelineIDs = null
    stopProgress?.()
    stopChanges?.()
    generation++
    loadRequest++
    resultsRequest++
    loadAbort.abort()
    daysAbort.abort()
    resultsAbort.abort()
  })

  /** Load the current scope's groups (and, for a search, its results). */
  async function load() {
    const s = scope.value
    const current = generation
    const request = ++loadRequest
    let revision = resultsRevision
    resultsAbort.abort()
    resultsRequest++
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
        // A pre-change answer must be read again before any of its hits
        // reach the grid. Repeat if another change arrived during the read.
        while (revision !== resultsRevision) {
          revision = resultsRevision
          items = await currentHits(items, signal)
          if (current !== generation || request !== loadRequest) return
        }
        // A refresh of the previous results must not replace these hits.
        resultsAbort.abort()
        resultsRequest++
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
    daysError.value = '' // the grid asks for its days again
  }

  /** Search hits as they are now (see engine.searchHits), each keeping the moment it was found at. */
  async function currentHits(items: GridItem[], signal: AbortSignal): Promise<GridItem[]> {
    const moments = new Map(items.map((item) => [item.id, item.at]))
    const hits = await engine.searchHits(
      items.map((item) => item.id),
      signal,
    )
    return hits.map((item) => {
      const at = moments.get(item.id)
      return at ? { ...item, at } : item
    })
  }

  async function refreshResults() {
    const current = generation
    const load = loadRequest
    const request = ++resultsRequest
    resultsAbort.abort()
    resultsAbort = new AbortController()
    const items = days.value.get(RESULTS) ?? []
    try {
      const kept = await currentHits(items, resultsAbort.signal)
      if (current !== generation || load !== loadRequest || request !== resultsRequest) return
      buckets.value = kept.length ? [{ day: RESULTS, count: kept.length }] : []
      days.value = new Map(kept.length ? [[RESULTS, kept]] : [])
      if (selected.value && !itemAt(selected.value)) selected.value = movedOn(selected.value)
      if (viewer.value && !itemAt(viewer.value)) viewer.value = movedOn(viewer.value)
      stale = false
    } catch (e) {
      if (current !== generation || request !== resultsRequest || isCancelled(e)) return
      throw e
    }
  }

  /**
   * Reload after the engine reports changes (all, if c is not given): counts,
   * then visible days on demand. Search results are read again in place: they
   * lose hidden hits and show new thumbnails; searching again picks up new
   * photos without reshuffling a live search.
   */
  let refreshing: Promise<void> | null = null
  let refreshRevision = 0
  async function refresh(c?: Changes) {
    const s = scope.value
    if (s.kind === 'search') {
      if (c && !c.assets && !c.thumbs) return
      resultsRevision++
      if (!shown) {
        resultsAbort.abort()
        resultsRequest++
        stale = true
        return
      }
      return refreshResults()
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
    clearSelection()
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
   * logged and said in daysError.
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
      daysError.value = ''
      // A filter edit or source removal can remove the selected item while
      // leaving its day in place. Do not leave a panel with inactive actions.
      if (selected.value && missing.includes(selected.value.day) && !itemAt(selected.value))
        selected.value = movedOn(selected.value)
      if (viewer.value && missing.includes(viewer.value.day) && !itemAt(viewer.value))
        viewer.value = movedOn(viewer.value)
    } catch (e) {
      if (isCancelled(e)) return
      logError(e)
      if (current === generation && revision === dayRevision) daysError.value = errorText(e)
    }
  }

  /** Ask again for the days the grid shows, after a load of them failed. */
  function retryDays() {
    daysError.value = ''
    notifyDays()
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
    clearSelection()
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
    if (await removeFromView(p, () => engine.trashAssets([p.id]))) showTrashUndo([p.id], restoreAssets)
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
    settingErrors,
    saveVideos,
    saveFormatExcluded,
    reloadSettings,
    resetPreferences,
    saveAIOnBattery,
    savePauseFullScreen,
    savePauseBusy,
    saveIdleOnly,
    saveOCR,
    saveXMP,
    saveEventGap,
    saveThumbCacheMB,
    saveCPUThreads,
    intendTagConfidence,
    saveTagConfidence,
    progress,
    scope,
    buckets,
    timelineLoaded,
    loadError,
    daysError,
    searchInfo,
    days,
    selected,
    selectedIDs,
    selectionSnapshot,
    selectVisible,
    selectItems,
    selectAllResults,
    clearSelection,
    viewer,
    showDetails,
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
    retryDays,
    neighbour,
    setScope,
    attach,
    detach,
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

type Library = ReturnType<typeof useLibrary>

/** What the Viewer's bar offers besides browsing; an action that is absent is not offered. */
export interface ViewerActions {
  favourite?: () => void
  moveToTrash?: () => void
  restore?: () => void
  deleteFromDisk?: () => void
  /** Whether the details panel, and the key that toggles it, are offered. */
  details: boolean
}

/**
 * Where the Viewer's photos come from: the library grid (and the Trash, which
 * is the grid showing trashed photos) or the Blocked screen's browser. The
 * Viewer asks its source for the photo shown and the one beside it, and for
 * what it may do with it, so it knows nothing of days or blocks.
 */
export interface ViewerSource {
  first?: () => Promise<void>
  /** A bounded neighbourhood, in this source's order. */
  window?: () => Promise<GridItem[]>
  jump?: (id: number) => void
  /** The id of the photo shown, even before its item has loaded. */
  readonly id: number | null
  /** The photo shown, or null while it is not in the source's items. */
  readonly item: GridItem | null
  /** The photo as the library grid addresses it, for the details panel; null for a source without one. */
  readonly sel: Sel | null
  /** Moves to the next (1) or previous (-1) photo, if there is one. */
  step: (dir: 1 | -1) => Promise<void>
  /** Closes the Viewer. */
  close: () => void
  /** Called as the photo is looked at (a video playing), by sources whose screen locks when unused. */
  touch?: () => void
  readonly actions: ViewerActions
}

/**
 * The library grid as a Viewer source. It reads the store at each call, so a
 * change of scope or an item reloaded in place is seen as it happens.
 */
export function libraryViewerSource(lib: Library): ViewerSource {
  return {
    async first() {
      const day = lib.buckets[0]?.day
      const from = lib.viewer
      if (!day || !from) return
      await lib.ensureDays([day])
      if (lib.viewer === from) lib.viewer = lib.selAt(day, 0)
    },
    async window() {
      const anchor = lib.viewer
      if (!anchor) return []
      const before: GridItem[] = []
      const after: GridItem[] = []
      for (const dir of [-1, 1] as const) {
        let cursor: Sel | null = anchor
        for (let i = 0; i < 12 && cursor; i++) {
          cursor = await lib.neighbour(cursor, dir)
          if (lib.viewer !== anchor) return []
          const item = lib.itemAt(cursor)
          if (item) {
            if (dir === -1) before.unshift(item)
            else after.push(item)
          }
        }
      }
      const current = lib.itemAt(anchor)
      return [...before, ...(current ? [current] : []), ...after]
    },
    jump(id) {
      for (const [day, items] of lib.days) {
        if (items.some((item) => item.id === id)) {
          lib.viewer = { day, id }
          return
        }
      }
    },
    get id() {
      return lib.viewer?.id ?? null
    },
    get item() {
      return lib.viewerItem
    },
    get sel() {
      return lib.viewer
    },
    async step(dir) {
      const from = lib.viewer
      if (!from) return
      const next = await lib.neighbour(from, dir)
      if (next && lib.viewer === from) lib.viewer = next // not if the viewer was closed or moved meanwhile
    },
    close() {
      lib.viewer = null
    },
    get actions() {
      // The Trash's photos can be restored or deleted from disk, not edited.
      return lib.inTrash
        ? {
            restore: () => {
              if (lib.viewer) void lib.restoreFromTrash(lib.viewer)
            },
            deleteFromDisk: () => {
              if (lib.viewer) void deleteFromDisk(lib, lib.viewer)
            },
            details: true,
          }
        : {
            favourite: () => void lib.toggleFavourite(lib.viewer),
            moveToTrash: () => {
              if (lib.viewer) void lib.moveToTrash(lib.viewer)
            },
            details: true,
          }
    },
  }
}
