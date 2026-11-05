import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { effectScope, nextTick, ref } from 'vue'
import { engine, ErrorCode, Kind } from '../bridge'
import type * as Bridge from '../bridge'
import { deferred } from '../shared/testing'
import { showBlocksChanged } from '../shared/block'
import { emptyRule } from './store'
import { useAlbumPreview } from './preview'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { timeline: vi.fn(), days: vi.fn(), previewAlbum: vi.fn() },
}))
let active: ReturnType<typeof effectScope> | undefined
function preview(rule = ref<Bridge.AlbumRule | null>(emptyRule())) {
  active = effectScope()
  const result = active.run(() => useAlbumPreview(() => rule.value))
  if (!result) throw new Error('The preview did not start')
  return result
}
const sample = [1, 2, 3].map((id) => ({ id, w: 1, h: 1, t: 1 }))
beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.previewAlbum).mockReset().mockResolvedValue({ count: 0, items: [] })
  vi.mocked(engine.timeline).mockReset()
  vi.mocked(engine.days).mockReset()
})
afterEach(() => {
  active?.stop()
  vi.useRealTimers()
})

it('counts and samples 100 matching days without reading every day', async () => {
  vi.mocked(engine.timeline).mockResolvedValue(
    Array.from({ length: 100 }, (_, i) => ({
      day: new Date(Date.UTC(2026, 0, 100 - i)).toISOString().slice(0, 10),
      count: 3,
    })),
  )
  vi.mocked(engine.days).mockImplementation((from) => Promise.resolve([{ day: from, items: sample }]))
  vi.mocked(engine.previewAlbum).mockResolvedValue({ count: 300, items: sample })
  const rule = { ...emptyRule(), kind: Kind.KindPhoto }
  const result = preview(ref(rule))
  await result.refresh()
  expect(result.count.value).toBe(300)
  expect(result.items.value.map((item) => item.id)).toEqual([1, 2, 3])
  expect(engine.days).not.toHaveBeenCalled()
  expect(engine.timeline).not.toHaveBeenCalled()
  expect(engine.previewAlbum).toHaveBeenCalledExactlyOnceWith(rule, expect.any(AbortSignal))
})

it('passes inclusive dates and Thing/person intersections to the canonical engine rule', async () => {
  const rule = { ...emptyRule(), from: '2026-10-15', to: '2026-10-16', tag: 4, person: 8 }
  vi.mocked(engine.previewAlbum).mockResolvedValue({ count: 6, items: sample })
  const result = preview(ref(rule))
  await result.refresh()
  expect(engine.previewAlbum).toHaveBeenCalledExactlyOnceWith(rule, expect.any(AbortSignal))
  expect(result.count.value).toBe(6)
  expect(result.items.value.map((item) => item.id)).toEqual([1, 2, 3])
})

it('retains both media and favourites in independent rule snapshots', async () => {
  vi.mocked(engine.previewAlbum)
    .mockResolvedValueOnce({ count: 2, items: sample.filter((item) => item.id !== 2) })
    .mockResolvedValueOnce({ count: 1, items: [{ id: 2, w: 1, h: 1, t: 1, v: true }] })
  const rule = ref({ ...emptyRule(), favourites: true, kind: Kind.KindPhoto })
  const result = preview(rule)
  await result.refresh()
  expect(result.count.value).toBe(2)
  expect(result.items.value.map((item) => item.id)).toEqual([1, 3])
  rule.value = { ...rule.value, kind: Kind.KindVideo }
  await nextTick()
  await result.refresh()
  expect(result.count.value).toBe(1)
  expect(result.items.value.map((item) => item.id)).toEqual([2])
  expect(vi.mocked(engine.previewAlbum).mock.calls.map(([rule]) => rule)).toEqual([
    { ...emptyRule(), favourites: true, kind: Kind.KindPhoto },
    { ...emptyRule(), favourites: true, kind: Kind.KindVideo },
  ])
})

it('drops superseded results and aborts old work before the next draft loads', async () => {
  let finish: ((result: Bridge.AlbumPreview) => void) | undefined
  vi.mocked(engine.previewAlbum).mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
  const rule = ref<Bridge.AlbumRule | null>(emptyRule())
  const result = preview(rule)
  const old = result.refresh()
  const signal = vi.mocked(engine.previewAlbum).mock.calls[0]?.[1]
  rule.value = { ...emptyRule(), tag: 6 }
  await nextTick()
  expect(signal?.aborted).toBe(true)
  expect(result.items.value).toEqual([])
  await result.refresh()
  if (!finish) throw new Error('The first read did not start')
  finish({ count: 99, items: sample })
  await old
  expect(result.count.value).toBe(0)
  expect(result.items.value).toEqual([])
  rule.value = null
  await nextTick()
  expect(result.count.value).toBeNull()
  expect(result.items.value).toEqual([])
})

it('clears private previews immediately even when the next read fails, then retries the current rule', async () => {
  vi.mocked(engine.previewAlbum).mockResolvedValueOnce({ count: 3, items: sample })
  const result = preview()
  await result.refresh()
  expect(result.items.value).toHaveLength(3)
  const signal = vi.mocked(engine.previewAlbum).mock.calls[0]?.[1]
  vi.mocked(engine.previewAlbum).mockRejectedValueOnce(new Error('read refused'))
  const changed = showBlocksChanged()
  expect(result.items.value).toEqual([])
  expect(result.count.value).toBeNull()
  expect(signal?.aborted).toBe(true)
  await changed
  expect(result.items.value).toEqual([])
  expect(result.error.value).toContain('read refused')
  await result.refresh()
  expect(result.count.value).toBe(0)
  expect(result.error.value).toBe('')
})

it('debounces rapid edits and cancels the final request when its owner closes', async () => {
  vi.useFakeTimers()
  const rule = ref<Bridge.AlbumRule | null>(emptyRule())
  const result = preview(rule)
  for (const tag of [1, 2, 3]) {
    rule.value = { ...emptyRule(), tag }
    await nextTick()
    await vi.advanceTimersByTimeAsync(50)
  }
  expect(engine.previewAlbum).not.toHaveBeenCalled()
  await vi.advanceTimersByTimeAsync(150)
  expect(engine.previewAlbum).toHaveBeenCalledExactlyOnceWith({ ...emptyRule(), tag: 3 }, expect.any(AbortSignal))
  expect(result.loading.value).toBe(false)
  const signal = vi.mocked(engine.previewAlbum).mock.calls[0]?.[1]
  active?.stop()
  expect(signal?.aborted).toBe(true)
})

it('does not retire current references after a superseded preview refuses an older reference', async () => {
  const pending = deferred<Bridge.AlbumPreview>()
  vi.mocked(engine.previewAlbum).mockReturnValueOnce(pending.promise)
  const rule = ref<Bridge.AlbumRule | null>({ ...emptyRule(), tag: 7 })
  const result = preview(rule)
  const older = result.refresh()
  rule.value = { ...emptyRule(), tag: 8 }
  await nextTick()
  await result.refresh()
  pending.reject(new Error('old reference unavailable', { cause: { code: ErrorCode.NotFound } }))
  await older
  expect(result.referenceUnavailable.value).toBe(false)
  expect(result.error.value).toBe('')
  expect(result.count.value).toBe(0)
})
