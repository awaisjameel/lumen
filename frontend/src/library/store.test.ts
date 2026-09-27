import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { engine, Filter } from '../bridge'
import type * as Bridge from '../bridge'
import type { DayBucket, DayItems, SearchResults } from '../bridge'
import { useLibrary } from './store'

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

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.timeline).mockReset().mockResolvedValue(buckets)
  vi.mocked(engine.days).mockReset()
  vi.mocked(engine.search).mockReset()
})

describe('library requests', () => {
  it('does not let a previous view block or overwrite the same day in a new view', async () => {
    const lib = useLibrary()
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
    const lib = useLibrary()
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
    const lib = useLibrary()
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
    const lib = useLibrary()
    const old = deferred<SearchResults>()
    vi.mocked(engine.search)
      .mockReturnValueOnce(old.promise)
      .mockResolvedValueOnce({
        items: result(2)[0]?.items ?? [],
        people: [],
        pending: 0,
        complete: true,
      })
    const scope = { kind: 'search', query: 'beach' } as const
    const pending = lib.setScope(scope)
    await lib.setScope(scope)
    old.resolve({ items: result(1)[0]?.items ?? [], people: [], pending: 0, complete: true })
    await pending
    expect(lib.days.get('results')?.[0]?.id).toBe(2)
  })

  it('exposes failed searches instead of leaving only an indefinite loading indicator', async () => {
    const lib = useLibrary()
    vi.mocked(engine.search).mockRejectedValueOnce(new Error('model unavailable'))
    await expect(lib.setScope({ kind: 'search', query: 'beach' })).rejects.toThrow('model unavailable')
    expect(lib.loadError).toContain('model unavailable')
  })

  it('does not drop a change received while refreshing', async () => {
    const lib = useLibrary()
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
})
