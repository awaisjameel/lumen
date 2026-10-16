import { defineComponent, ref } from 'vue'
import { createPinia } from 'pinia'
import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type * as Bridge from '../bridge'
import { engine, Filter } from '../bridge'
import { showBlocksChanged } from './block'
import { useCollectionPreview } from './collectionPreview'
import { mountInApp } from './testing'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { timeline: vi.fn(), days: vi.fn() },
}))
let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())

function mounted() {
  let sample: ReturnType<typeof useCollectionPreview> | undefined
  const scope = ref<Bridge.View>({ filter: Filter.FilterAll, place: 1 })
  const pinia = createPinia()
  const Host = defineComponent({
    setup() {
      sample = useCollectionPreview(() => scope.value)
      return () => null
    },
  })
  wrapper = mountInApp(Host, { plugins: [pinia] })
  if (!sample) throw new Error('Preview was not mounted')
  return { sample, scope }
}

it('reads only the newest day on demand and keeps at most five distinct photo choices', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([
    { day: '2026-10-01', count: 8 },
    { day: '2026-09-01', count: 2 },
  ])
  vi.mocked(engine.days).mockResolvedValue([
    { day: '2026-10-01', items: Array.from({ length: 8 }, (_, i) => ({ id: i + 1, w: 100, h: 100, t: 1 })) },
  ])
  const { sample } = mounted()
  expect(engine.days).not.toHaveBeenCalled()
  await sample.load()
  expect(engine.days).toHaveBeenCalledWith(
    '2026-10-01',
    '2026-10-01',
    { filter: Filter.FilterAll, place: 1 },
    expect.any(AbortSignal),
  )
  expect(sample.items.value).toHaveLength(5)
  expect(sample.items.value[0]?.day).toBe('2026-10-01')
  await sample.load()
  expect(engine.days).toHaveBeenCalledTimes(1)
})

it('drops an older response when its collection changes', async () => {
  let finish: ((value: Bridge.DayItems[]) => void) | undefined
  vi.mocked(engine.timeline).mockResolvedValue([{ day: '2026-10-01', count: 1 }])
  vi.mocked(engine.days).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const { sample, scope } = mounted()
  const pending = sample.load()
  await flushPromises()
  scope.value = { filter: Filter.FilterAll, place: 2 }
  await flushPromises()
  finish?.([{ day: '2026-10-01', items: [{ id: 1, w: 100, h: 100, t: 1 }] }])
  await pending
  expect(sample.items.value).toEqual([])
  expect(sample.loading.value).toBe(false)
})

it('clears private previews immediately when blocks change', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([{ day: '2026-10-01', count: 1 }])
  vi.mocked(engine.days).mockResolvedValue([{ day: '2026-10-01', items: [{ id: 1, w: 100, h: 100, t: 1 }] }])
  const { sample } = mounted()
  await sample.load()
  expect(sample.items.value).toHaveLength(1)
  await showBlocksChanged()
  expect(sample.items.value).toEqual([])
})

it('keeps a loaded mosaic when a card payload is replaced with the same scope', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([{ day: '2026-10-01', count: 1 }])
  vi.mocked(engine.days).mockResolvedValue([{ day: '2026-10-01', items: [{ id: 1, w: 100, h: 100, t: 1 }] }])
  const { sample, scope } = mounted()
  await sample.load()
  scope.value = { filter: Filter.FilterAll, place: 1 }
  await flushPromises()
  expect(sample.items.value).toHaveLength(1)
  await sample.load()
  expect(engine.days).toHaveBeenCalledTimes(1)
})
