import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick, watch } from 'vue'
import { Device, engine, Filter, onLibraryChanged, onProgress, startMedia } from '../bridge'
import type * as Bridge from '../bridge'
import type * as Errors from '../shared/errors'
import type { DayBucket, DayItems, Progress, SearchResults } from '../bridge'
import { affects, scopeKey, useLibrary } from './store'
import { reportError } from '../shared/errors'

vi.mock('../shared/errors', async (importOriginal) => ({
  ...(await importOriginal<typeof Errors>()),
  reportError: vi.fn(),
}))

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    timeline: vi.fn(),
    days: vi.fn(),
    search: vi.fn(),
    visibleAssets: vi.fn(),
    sources: vi.fn(),
    settings: vi.fn(),
    progress: vi.fn(),
    setFavourite: vi.fn(),
    setPaused: vi.fn(),
  },
  onLibraryChanged: vi.fn(),
  onProgress: vi.fn(),
  startMedia: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

const day = '2026-09-01'
const buckets: DayBucket[] = [{ day, count: 1 }]
const all = { kind: 'timeline', view: { filter: Filter.FilterAll } } as const
const favourites = { kind: 'timeline', view: { filter: Filter.FilterFavourites } } as const
const result = (id: number): DayItems[] => [{ day, items: [{ id, w: 100, h: 100, t: 1 }] }]
const progress: Progress = {
  scanning: false,
  total: 0,
  metadata: 0,
  thumbnail: 0,
  embedding: 0,
  faces: 0,
  errors: 0,
  skipped: 0,
  paused: false,
  onBattery: false,
  ai: { device: Device.DeviceCPU, error: '', facesError: '' },
}

/** The library store with a grid showing it, as LibraryView does. */
function shownLibrary() {
  const lib = useLibrary()
  lib.attach()
  return lib
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.timeline).mockReset().mockResolvedValue(buckets)
  vi.mocked(engine.days).mockReset()
  vi.mocked(engine.search).mockReset()
  vi.mocked(engine.visibleAssets).mockReset()
  vi.mocked(engine.sources).mockReset().mockResolvedValue([])
  vi.mocked(engine.settings)
    .mockReset()
    .mockResolvedValue({ videos: false, aiOnBattery: false, tagConfidence: 0.02, eventGapHours: 12 })
  vi.mocked(engine.progress).mockReset().mockResolvedValue(progress)
  vi.mocked(startMedia).mockReset().mockResolvedValue(undefined)
  vi.mocked(onProgress).mockReset().mockReturnValue(vi.fn())
  vi.mocked(onLibraryChanged).mockReset().mockReturnValue(vi.fn())
  vi.mocked(engine.setFavourite).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setPaused).mockReset().mockResolvedValue(undefined)
  vi.mocked(reportError).mockClear()
})

describe('library lifetime', () => {
  it('subscribes once across initialisation retries and releases both listeners on disposal', async () => {
    const stopProgress = vi.fn()
    const stopChanges = vi.fn()
    vi.mocked(onProgress).mockReturnValue(stopProgress)
    vi.mocked(onLibraryChanged).mockReturnValue(stopChanges)
    vi.mocked(engine.sources).mockRejectedValueOnce(new Error('not ready'))
    const lib = useLibrary()
    await expect(lib.init()).rejects.toThrow('not ready')
    await lib.init()
    await lib.init()
    expect(lib.ready).toBe(true)
    expect(onProgress).toHaveBeenCalledTimes(1)
    expect(onLibraryChanged).toHaveBeenCalledTimes(1)
    lib.$dispose()
    expect(stopProgress).toHaveBeenCalledTimes(1)
    expect(stopChanges).toHaveBeenCalledTimes(1)
  })

  it('cancels day reads and discards initialisation that finishes after disposal', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    const pendingDay = deferred<DayItems[]>()
    vi.mocked(engine.days).mockReturnValueOnce(pendingDay.promise)
    const dayLoad = lib.ensureDays([day])
    const signal = vi.mocked(engine.days).mock.calls[0]?.[3]
    const pendingSources = deferred<Bridge.Source[]>()
    vi.mocked(engine.sources).mockReturnValueOnce(pendingSources.promise)
    const initialising = lib.init()
    lib.$dispose()
    expect(signal?.aborted).toBe(true)
    pendingSources.resolve([])
    pendingDay.resolve(result(1))
    await Promise.all([initialising, dayLoad])
    expect(lib.ready).toBe(false)
    expect(lib.days.size).toBe(0)
  })
})

describe('optimistic actions', () => {
  it('restores the persisted favourite when two queued toggles both fail', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    vi.mocked(engine.days).mockResolvedValueOnce(result(1))
    await lib.ensureDays([day])
    let fail: (error: Error) => void = () => {
      throw new Error('the pending save has not started')
    }
    vi.mocked(engine.setFavourite)
      .mockReturnValueOnce(
        new Promise((_resolve, reject) => {
          fail = reject
        }),
      )
      .mockRejectedValueOnce(new Error('latest favourite failed'))
    const selected = { day, id: 1 }
    const first = lib.toggleFavourite(selected)
    const second = lib.toggleFavourite(selected)
    await nextTick()
    expect(engine.setFavourite).toHaveBeenCalledExactlyOnceWith(1, true)
    fail(new Error('older favourite failed'))
    await Promise.all([first, second])
    expect(engine.setFavourite).toHaveBeenLastCalledWith(1, false)
    expect(lib.itemAt(selected)?.f).toBe(false)
    expect(reportError).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ message: 'latest favourite failed' }))
  })

  it('serializes pause changes and restores the persisted state when both fail', async () => {
    const lib = useLibrary()
    lib.progress = progress
    let fail: (error: Error) => void = () => {
      throw new Error('the pending save has not started')
    }
    vi.mocked(engine.setPaused)
      .mockReturnValueOnce(
        new Promise((_resolve, reject) => {
          fail = reject
        }),
      )
      .mockRejectedValueOnce(new Error('latest pause failed'))
    const first = lib.setPaused(true)
    const second = lib.setPaused(false)
    expect(engine.setPaused).toHaveBeenCalledExactlyOnceWith(true)
    fail(new Error('older pause failed'))
    await Promise.all([first, second])
    expect(engine.setPaused).toHaveBeenLastCalledWith(false)
    expect(lib.progress.paused).toBe(false)
    expect(reportError).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ message: 'latest pause failed' }))
  })
})

describe('library requests', () => {
  it('keeps a pre-change search answer out of the grid until visibility is confirmed', async () => {
    const lib = shownLibrary()
    const search = deferred<SearchResults>()
    const visibility = deferred<number[]>()
    vi.mocked(engine.search).mockReturnValueOnce(search.promise)
    vi.mocked(engine.visibleAssets).mockResolvedValueOnce([]).mockReturnValueOnce(visibility.promise)
    const loading = lib.setScope({ kind: 'search', query: 'beach' })
    await lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    search.resolve({
      items: result(1)[0]?.items ?? [],
      people: [],
      pending: 0,
      complete: true,
      noAI: false,
      blocked: false,
    })
    await nextTick()
    expect(engine.visibleAssets).toHaveBeenLastCalledWith([1], expect.any(AbortSignal))
    expect(lib.days.size).toBe(0)
    expect(lib.total).toBe(0)
    visibility.resolve([])
    await loading
    expect(lib.days.size).toBe(0)
    expect(lib.timelineLoaded).toBe(true)
  })

  it('checks a pending search again when visibility changes during its check', async () => {
    const lib = shownLibrary()
    const search = deferred<SearchResults>()
    const visibility = deferred<number[]>()
    vi.mocked(engine.search).mockReturnValueOnce(search.promise)
    vi.mocked(engine.visibleAssets)
      .mockResolvedValueOnce([])
      .mockReturnValueOnce(visibility.promise)
      .mockResolvedValue([])
    const loading = lib.setScope({ kind: 'search', query: 'beach' })
    await lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    search.resolve({
      items: result(1)[0]?.items ?? [],
      people: [],
      pending: 0,
      complete: true,
      noAI: false,
      blocked: false,
    })
    await nextTick()
    await lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    visibility.resolve([1])
    await loading
    expect(engine.visibleAssets).toHaveBeenCalledTimes(4)
    expect(lib.days.size).toBe(0)
  })

  it('does not let a prune of previous results clear a newly verified search', async () => {
    const lib = shownLibrary()
    const search = deferred<SearchResults>()
    const oldPrune = deferred<number[]>()
    vi.mocked(engine.search).mockReturnValueOnce(search.promise)
    vi.mocked(engine.visibleAssets).mockReturnValueOnce(oldPrune.promise).mockResolvedValueOnce([1])
    const loading = lib.setScope({ kind: 'search', query: 'beach' })
    const refreshing = lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    search.resolve({
      items: result(1)[0]?.items ?? [],
      people: [],
      pending: 0,
      complete: true,
      noAI: false,
      blocked: false,
    })
    await loading
    expect(lib.days.get('results')?.[0]?.id).toBe(1)
    oldPrune.resolve([])
    await refreshing
    expect(lib.days.get('results')?.[0]?.id).toBe(1)
  })

  it('runs a queued timeline refresh even when the older read fails', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    let fail: (error: Error) => void = () => {
      throw new Error('the pending read has not started')
    }
    vi.mocked(engine.timeline)
      .mockReturnValueOnce(
        new Promise((_resolve, reject) => {
          fail = reject
        }),
      )
      .mockResolvedValueOnce([{ day, count: 2 }])
    const first = lib.refresh()
    const second = lib.refresh()
    fail(new Error('older read failed'))
    await Promise.all([first, second])
    expect(lib.total).toBe(2)
    expect(lib.loadError).toBe('')
    expect(engine.timeline).toHaveBeenCalledTimes(3)
  })

  it('prunes hidden search hits without changing order or rerunning the search', async () => {
    const lib = shownLibrary()
    vi.mocked(engine.search).mockResolvedValueOnce({
      items: [3, 1, 2].map((id) => ({ id, w: 100, h: 100, t: 1 })),
      people: ['Ali'],
      pending: 5,
      complete: false,
      noAI: false,
      blocked: false,
    })
    await lib.setScope({ kind: 'search', query: 'Ali' })
    lib.selected = { day: 'results', id: 1 }
    lib.viewer = { day: 'results', id: 1 }
    vi.mocked(engine.visibleAssets).mockResolvedValueOnce([2, 3])
    await lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    expect(lib.days.get('results')?.map((item) => item.id)).toEqual([3, 2])
    expect(lib.total).toBe(2)
    expect(lib.buckets).toEqual([{ day: 'results', count: 2 }])
    expect(lib.searchInfo).toEqual({ people: ['Ali'], pending: 5, complete: false, noAI: false, blocked: false })
    expect(lib.selected).toBeNull()
    expect(lib.viewer).toBeNull()
    expect(engine.search).toHaveBeenCalledTimes(1)
  })

  it('ignores a superseded visibility prune even if its response arrives last', async () => {
    const lib = shownLibrary()
    vi.mocked(engine.search).mockResolvedValueOnce({
      items: [1, 2, 3].map((id) => ({ id, w: 100, h: 100, t: 1 })),
      people: [],
      pending: 0,
      complete: true,
      noAI: false,
      blocked: false,
    })
    await lib.setScope({ kind: 'search', query: 'beach' })
    const old = deferred<number[]>()
    vi.mocked(engine.visibleAssets).mockReturnValueOnce(old.promise).mockResolvedValueOnce([2, 3])
    const first = lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    const signal = vi.mocked(engine.visibleAssets).mock.calls[0]?.[1]
    await lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    expect(signal?.aborted).toBe(true)
    old.resolve([1])
    await first
    expect(lib.days.get('results')?.map((item) => item.id)).toEqual([2, 3])
  })

  it('clears the search bucket when no current hits remain visible', async () => {
    const lib = shownLibrary()
    vi.mocked(engine.search).mockResolvedValueOnce({
      items: result(1)[0]?.items ?? [],
      people: [],
      pending: 0,
      complete: true,
      noAI: false,
      blocked: false,
    })
    await lib.setScope({ kind: 'search', query: 'beach' })
    vi.mocked(engine.visibleAssets).mockResolvedValueOnce([])
    await lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    expect(lib.days.size).toBe(0)
    expect(lib.buckets).toEqual([])
    expect(lib.total).toBe(0)
  })

  it('marks a detached search stale without reading visibility until shown again', async () => {
    const lib = shownLibrary()
    vi.mocked(engine.search).mockResolvedValue({
      items: [],
      people: [],
      pending: 0,
      complete: true,
      noAI: false,
      blocked: false,
    })
    await lib.setScope({ kind: 'search', query: 'beach' })
    lib.detach()
    await lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    expect(engine.visibleAssets).not.toHaveBeenCalled()
    lib.attach()
    await lib.setScope({ kind: 'search', query: 'beach' })
    expect(engine.search).toHaveBeenCalledTimes(2)
  })

  it('prunes a pre-change search response that was still loading', async () => {
    const lib = shownLibrary()
    const pending = deferred<SearchResults>()
    vi.mocked(engine.search).mockReturnValueOnce(pending.promise)
    vi.mocked(engine.visibleAssets).mockResolvedValue([])
    const loading = lib.setScope({ kind: 'search', query: 'beach' })
    await lib.refresh({ assets: true, thumbs: false, tags: false, people: false })
    pending.resolve({
      items: result(1)[0]?.items ?? [],
      people: [],
      pending: 0,
      complete: true,
      noAI: false,
      blocked: false,
    })
    await loading
    expect(lib.days.size).toBe(0)
    expect(lib.total).toBe(0)
    expect(engine.search).toHaveBeenCalledTimes(1)
  })

  it('loads a day again when a thumbs-only refresh cancels its pending read', async () => {
    const { CancelError } = await vi.importActual<{ CancelError: new (message: string) => Error }>('@wailsio/runtime')
    const lib = shownLibrary()
    await lib.setScope(all)
    let calls = 0
    vi.mocked(engine.days).mockImplementation(
      (_from, _to, _view, signal) =>
        new Promise((resolve, reject) => {
          calls++
          if (calls === 1) signal?.addEventListener('abort', () => reject(new CancelError('stale')))
          else resolve(result(calls))
        }),
    )
    const stop = watch(
      () => lib.days,
      () => void lib.ensureDays([day]),
      { flush: 'post' },
    )
    try {
      void lib.ensureDays([day])
      await nextTick()
      await lib.refresh({ assets: false, thumbs: true, tags: false, people: false })
      for (let i = 0; i < 20; i++) await new Promise((resolve) => setTimeout(resolve, 0))
      expect(calls).toBeGreaterThan(1)
      expect(lib.days.get(day)?.[0]?.id).toBeGreaterThan(1)
    } finally {
      stop()
    }
  })
  it('cancels a superseded timeline read in the engine, without reporting it', async () => {
    // What the runtime rejects a cancelled call with (the bridge alone imports the runtime).
    const { CancelError } = await vi.importActual<{ CancelError: new (message: string) => Error }>('@wailsio/runtime')
    const lib = shownLibrary()
    const signals: (AbortSignal | undefined)[] = []
    vi.mocked(engine.timeline).mockImplementation(
      (_view, signal) =>
        new Promise((resolve, reject) => {
          signals.push(signal)
          signal?.addEventListener('abort', () => reject(new CancelError('superseded')))
          if (signals.length > 1) resolve(buckets)
        }),
    )
    const first = lib.setScope(all)
    await lib.setScope(favourites)
    await first
    expect(signals[0]?.aborted).toBe(true)
    expect(signals[1]?.aborted).toBe(false)
    expect(lib.loadError).toBe('')
    expect(lib.buckets).toEqual(buckets)
  })
  it('keeps place and event timelines in separate request scopes', () => {
    const place = scopeKey({ kind: 'timeline', view: { filter: Filter.FilterAll, place: 12 } })
    const event = scopeKey({ kind: 'timeline', view: { filter: Filter.FilterAll, event: 12 } })
    expect(place).not.toBe(event)
    expect(place).not.toBe(scopeKey(all))
  })
  it('does not let a previous view block or overwrite the same day in a new view', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    const old = deferred<DayItems[]>()
    vi.mocked(engine.days).mockReturnValueOnce(old.promise).mockResolvedValueOnce(result(2))
    const pending = lib.ensureDays([day])
    await lib.setScope(favourites)
    await lib.ensureDays([day])
    expect(lib.days.get(day)?.[0]?.id).toBe(2)
    old.resolve(result(1))
    await pending
    expect(lib.days.get(day)?.[0]?.id).toBe(2)
  })

  it('discards an old response after navigating away and back', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    const old = deferred<DayItems[]>()
    vi.mocked(engine.days).mockReturnValueOnce(old.promise).mockResolvedValueOnce(result(3))
    const pending = lib.ensureDays([day])
    await lib.setScope(favourites)
    await lib.setScope(all)
    await lib.ensureDays([day])
    old.resolve(result(1))
    await pending
    expect(lib.days.get(day)?.[0]?.id).toBe(3)
  })

  it('invalidates an in-flight day when the library changes', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    const old = deferred<DayItems[]>()
    vi.mocked(engine.days).mockReturnValueOnce(old.promise).mockResolvedValueOnce(result(2))
    const pending = lib.ensureDays([day])
    await lib.refresh()
    old.resolve(result(1))
    await pending
    await lib.ensureDays([day])
    expect(lib.days.get(day)?.[0]?.id).toBe(2)
  })

  it('keeps the newest repeated search when responses arrive out of order', async () => {
    const lib = shownLibrary()
    const old = deferred<SearchResults>()
    vi.mocked(engine.search)
      .mockReturnValueOnce(old.promise)
      .mockResolvedValueOnce({
        items: result(2)[0]?.items ?? [],
        people: [],
        pending: 0,
        complete: true,
        noAI: false,
        blocked: false,
      })
    const scope = { kind: 'search', query: 'beach' } as const
    const pending = lib.setScope(scope)
    await lib.setScope(scope)
    old.resolve({
      items: result(1)[0]?.items ?? [],
      people: [],
      pending: 0,
      complete: true,
      noAI: false,
      blocked: false,
    })
    await pending
    expect(lib.days.get('results')?.[0]?.id).toBe(2)
  })

  it('removes blocked search hits immediately after a visibility change', async () => {
    const lib = shownLibrary()
    vi.mocked(engine.search)
      .mockResolvedValueOnce({
        items: result(1)[0]?.items ?? [],
        people: [],
        pending: 0,
        complete: true,
        noAI: false,
        blocked: false,
      })
      .mockResolvedValueOnce({ items: [], people: [], pending: 0, complete: true, noAI: false, blocked: true })
    await lib.setScope({ kind: 'search', query: 'Ali' })
    expect(lib.days.get('results')?.[0]?.id).toBe(1)
    await lib.refreshVisibility()
    expect(lib.days.get('results')).toBeUndefined()
    expect(lib.searchInfo?.blocked).toBe(true)
  })

  it('exposes failed searches instead of leaving only an indefinite loading indicator', async () => {
    const lib = shownLibrary()
    vi.mocked(engine.search).mockRejectedValueOnce(new Error('model unavailable'))
    await expect(lib.setScope({ kind: 'search', query: 'beach' })).rejects.toThrow('model unavailable')
    expect(lib.loadError).toContain('model unavailable')
  })

  it('clears rendered timeline media before a visibility reload finishes', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    vi.mocked(engine.days).mockResolvedValueOnce(result(1))
    await lib.ensureDays([day])
    lib.selected = { day, id: 1 }
    lib.viewer = { day, id: 1 }
    const pending = deferred<DayBucket[]>()
    vi.mocked(engine.timeline).mockReturnValueOnce(pending.promise)
    const refresh = lib.refreshVisibility()
    expect(lib.days.size).toBe(0)
    expect(lib.selected).toBeNull()
    expect(lib.viewer).toBeNull()
    pending.resolve(buckets)
    await refresh
  })

  it('discards pre-block day requests even while the grid is detached', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    const old = deferred<DayItems[]>()
    vi.mocked(engine.days).mockReturnValueOnce(old.promise).mockResolvedValueOnce(result(2))
    const pending = lib.ensureDays([day])
    lib.detach()
    await lib.refreshVisibility()
    old.resolve(result(1))
    await pending
    expect(lib.days.size).toBe(0)
    lib.attach()
    await lib.setScope(all)
    await lib.ensureDays([day])
    expect(lib.days.get(day)?.[0]?.id).toBe(2)
  })

  it('does not drop a change received while refreshing', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    const old = deferred<DayBucket[]>()
    vi.mocked(engine.timeline)
      .mockReturnValueOnce(old.promise)
      .mockResolvedValueOnce([{ day, count: 2 }])
    const first = lib.refresh()
    const second = lib.refresh()
    old.resolve(buckets)
    await Promise.all([first, second])
    expect(lib.total).toBe(2)
  })

  it('clears a removed selection and viewer even when their day still contains other items', async () => {
    const lib = shownLibrary()
    await lib.setScope(favourites)
    vi.mocked(engine.days).mockResolvedValueOnce(result(1)).mockResolvedValueOnce(result(2))
    await lib.ensureDays([day])
    lib.selected = { day, id: 1 }
    lib.viewer = { day, id: 1 }
    await lib.refresh()
    await lib.ensureDays([day])
    expect(lib.selected).toBeNull()
    expect(lib.viewer).toBeNull()
  })

  it('clears the cached day and selection when a filtered day disappears', async () => {
    const lib = shownLibrary()
    await lib.setScope(favourites)
    vi.mocked(engine.days).mockResolvedValueOnce(result(1))
    await lib.ensureDays([day])
    lib.selected = { day, id: 1 }
    lib.viewer = { day, id: 1 }
    vi.mocked(engine.timeline).mockResolvedValueOnce([])
    await lib.refresh()
    expect(lib.days.has(day)).toBe(false)
    expect(lib.selected).toBeNull()
    expect(lib.viewer).toBeNull()
  })

  it('waits for a day already loading instead of giving up on it', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    const old = deferred<DayItems[]>()
    vi.mocked(engine.days).mockReturnValueOnce(old.promise)
    const first = lib.ensureDays([day])
    const second = lib.ensureDays([day]) // e.g. the viewer's next photo while the grid loads it
    old.resolve(result(1))
    await second
    expect(lib.days.get(day)?.[0]?.id).toBe(1)
    await first
    expect(engine.days).toHaveBeenCalledTimes(1)
  })

  it('asks again for the days shown, not the timeline, when only thumbnails changed', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    vi.mocked(engine.days).mockResolvedValue(result(1))
    await lib.ensureDays([day])
    // The grid asks for what it shows whenever days changes (LibraryView's
    // watcher, a getter: it hears of a new Map, not of triggerRef).
    watch([() => lib.days], () => void lib.ensureDays([day]), { flush: 'post' })
    await lib.refresh({ assets: false, thumbs: true, tags: false, people: false })
    for (let i = 0; i < 5; i++) await nextTick()
    expect(engine.timeline).toHaveBeenCalledTimes(1)
    expect(engine.days).toHaveBeenCalledTimes(2)
  })

  it('reloads a scope that changed while no grid showed it, when shown again', async () => {
    const lib = shownLibrary()
    await lib.setScope(all)
    lib.detach()
    vi.mocked(engine.timeline).mockResolvedValueOnce([{ day, count: 5 }])
    await lib.refresh()
    expect(lib.total).toBe(1) // nothing loaded while hidden
    lib.attach()
    await lib.setScope(all)
    expect(lib.total).toBe(5)
  })
})

describe('what a change affects', () => {
  const none = { assets: false, thumbs: false, tags: false, people: false }
  it('reloads the timeline only for changes it shows', () => {
    expect(affects(all.view, { ...none, tags: true })).toBe(false)
    expect(affects(all.view, { ...none, assets: true })).toBe(true)
    expect(affects(all.view, { ...none, thumbs: true })).toBe(false) // the days on screen reload instead
    expect(affects({ filter: Filter.FilterAll, tag: 3 }, { ...none, tags: true })).toBe(true)
    expect(affects({ filter: Filter.FilterAll, person: 3 }, { ...none, tags: true })).toBe(false)
    expect(affects({ filter: Filter.FilterAll, album: 3 }, { ...none, people: true })).toBe(true)
  })
})
