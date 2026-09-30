import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { engine, Filter } from '../bridge'
import type * as Bridge from '../bridge'
import type { DayBucket, DayItems, SearchResults } from '../bridge'
import { affects, scopeKey, useLibrary } from './store'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { timeline: vi.fn(), days: vi.fn(), search: vi.fn() },
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
})

describe('library requests', () => {
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
    await lib.refresh({ assets: false, thumbs: true, tags: false, people: false })
    expect(engine.timeline).toHaveBeenCalledTimes(1)
    expect(lib.needsDays([day])).toBe(true)
    await lib.ensureDays([day])
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
