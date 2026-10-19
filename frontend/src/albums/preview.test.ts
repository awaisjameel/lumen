import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { effectScope, nextTick, ref } from 'vue'
import { engine, Filter, Kind } from '../bridge'
import type * as Bridge from '../bridge'
import { showBlocksChanged } from '../shared/block'
import { emptyRule } from './store'
import { useAlbumPreview } from './preview'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { timeline: vi.fn(), days: vi.fn() },
}))
const scope = () => effectScope()
let active: ReturnType<typeof scope> | undefined
function preview(rule = ref<Bridge.AlbumRule | null>(emptyRule())) {
  active = scope()
  const result = active.run(() => useAlbumPreview(() => rule.value))
  if (!result) throw new Error('The preview did not start')
  return result
}
beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.timeline).mockReset().mockResolvedValue([])
  vi.mocked(engine.days).mockReset().mockResolvedValue([])
})
afterEach(() => active?.stop())

it('uses inclusive date bounds and the existing visibility filters for counts and three images', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([
    { day: '2026-10-17', count: 2 },
    { day: '2026-10-16', count: 4 },
    { day: '2026-10-15', count: 2 },
    { day: '2026-10-14', count: 1 },
  ])
  vi.mocked(engine.days).mockResolvedValue([
    { day: '2026-10-16', items: [1, 2, 3, 4].map((id) => ({ id, w: 1, h: 1, t: 1 })) },
  ])
  const result = preview(ref({ ...emptyRule(), from: '2026-10-15', to: '2026-10-16', tag: 4, person: 8 }))
  await result.refresh()
  expect(engine.timeline).toHaveBeenCalledWith({ filter: Filter.FilterAll, tag: 4, person: 8 }, expect.any(AbortSignal))
  expect(result.count.value).toBe(6)
  expect(result.items.value.map((item) => item.id)).toEqual([1, 2, 3])
  expect(engine.days).toHaveBeenCalledExactlyOnceWith(
    '2026-10-16',
    '2026-10-16',
    expect.anything(),
    expect.any(AbortSignal),
  )
})

it('intersects favourites with media kind without counting videos as photos', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([{ day: '2026-10-16', count: 3 }])
  vi.mocked(engine.days).mockResolvedValue([
    {
      day: '2026-10-16',
      items: [
        { id: 1, w: 1, h: 1, t: 1 },
        { id: 2, w: 1, h: 1, t: 1, v: true },
        { id: 3, w: 1, h: 1, t: 1 },
      ],
    },
  ])
  const rule = ref({ ...emptyRule(), favourites: true, kind: Kind.KindPhoto })
  const result = preview(rule)
  await result.refresh()
  expect(engine.timeline).toHaveBeenLastCalledWith(
    { filter: Filter.FilterFavourites, tag: 0, person: 0 },
    expect.any(AbortSignal),
  )
  expect(result.count.value).toBe(2)
  expect(result.items.value.map((item) => item.id)).toEqual([1, 3])
  rule.value = { ...rule.value, kind: Kind.KindVideo }
  await nextTick()
  await result.refresh()
  expect(result.count.value).toBe(1)
  expect(result.items.value.map((item) => item.id)).toEqual([2])
})

it('drops a superseded result and clears old images before the next draft loads', async () => {
  let finish: ((days: Bridge.DayBucket[]) => void) | undefined
  vi.mocked(engine.timeline).mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve
    }),
  )
  const rule = ref<Bridge.AlbumRule | null>(emptyRule())
  const result = preview(rule)
  const old = result.refresh()
  rule.value = { ...emptyRule(), tag: 6 }
  await nextTick()
  await result.refresh()
  if (!finish) throw new Error('The first read did not start')
  finish([{ day: '2026-10-16', count: 99 }])
  await old
  expect(result.count.value).toBe(0)
  expect(result.items.value).toEqual([])
  rule.value = null
  await nextTick()
  expect(result.count.value).toBeNull()
  expect(result.items.value).toEqual([])
})

it('clears a preview immediately when blocks change and never restores it after a failed refresh', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([{ day: '2026-10-16', count: 1 }])
  vi.mocked(engine.days).mockResolvedValue([{ day: '2026-10-16', items: [{ id: 1, w: 1, h: 1, t: 1 }] }])
  const result = preview()
  await result.refresh()
  expect(result.items.value).toHaveLength(1)
  vi.mocked(engine.timeline).mockRejectedValue(new Error('read refused'))
  const changed = showBlocksChanged()
  expect(result.items.value).toEqual([])
  await changed
  expect(result.count.value).toBeNull()
  expect(result.error.value).toContain('read refused')
})
