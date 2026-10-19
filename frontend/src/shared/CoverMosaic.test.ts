import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, nextTick, ref } from 'vue'
import { engine, Filter } from '../bridge'
import type * as Bridge from '../bridge'
import { showBlocksChanged } from './block'
import { mountInApp } from './testing'
import CoverMosaic from './CoverMosaic.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { timeline: vi.fn(), days: vi.fn() },
}))
const album = {
  view: { filter: Filter.FilterAll, album: 4 },
  count: 5,
  cover: 1,
  coverThumb: 1,
  icon: 'albums' as const,
}
let wrapper: ReturnType<typeof mountInApp> | undefined
beforeEach(() => {
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      constructor(private readonly callback: (entries: { isIntersecting: boolean }[]) => void) {}
      observe() {
        this.callback([{ isIntersecting: true }])
      }
      disconnect = vi.fn()
    },
  )
  vi.mocked(engine.timeline)
    .mockReset()
    .mockResolvedValue([{ day: '2026-10-16', count: 5 }])
  vi.mocked(engine.days)
    .mockReset()
    .mockResolvedValue([{ day: '2026-10-16', items: [1, 2, 3, 4, 5].map((id) => ({ id, t: 2, w: 1, h: 1 })) }])
})
afterEach(() => {
  wrapper?.unmount()
  vi.unstubAllGlobals()
})

it('loads at most three mosaic images only when the cover enters view', async () => {
  wrapper = mountInApp(CoverMosaic, { props: album })
  await flushPromises()
  expect(engine.timeline).toHaveBeenCalledWith({ filter: Filter.FilterAll, album: 4 }, expect.any(AbortSignal))
  expect(wrapper.findAll('img')).toHaveLength(3)
  expect(wrapper.findAll('img').every((img) => img.attributes('alt') === '')).toBe(true)
})

it('uses a single existing cover without extra reads for a one-item collection', async () => {
  wrapper = mountInApp(CoverMosaic, { props: { ...album, count: 1 } })
  await flushPromises()
  expect(engine.timeline).not.toHaveBeenCalled()
  expect(wrapper.findAll('img')).toHaveLength(1)
})

it('clears old mosaic images before loading a changed collection', async () => {
  const current = ref(album)
  const Host = defineComponent({ setup: () => () => h(CoverMosaic, current.value) })
  wrapper = mountInApp(Host)
  await flushPromises()
  vi.mocked(engine.timeline).mockReturnValue(new Promise(() => undefined))
  current.value = { ...album, cover: 6, coverThumb: 3 }
  await nextTick()
  expect(wrapper.findAll('img')).toHaveLength(1)
  expect(wrapper.get('img').attributes('src')).toContain('/6')
})

it('draws any view, such as Favourites, and falls back to its icon', async () => {
  vi.mocked(engine.days).mockResolvedValue([{ day: '2026-10-16', items: [] }])
  wrapper = mountInApp(CoverMosaic, { props: { view: { filter: Filter.FilterFavourites }, count: 12, icon: 'heart' } })
  await flushPromises()
  expect(engine.timeline).toHaveBeenCalledWith({ filter: Filter.FilterFavourites }, expect.any(AbortSignal))
  expect(wrapper.find('img').exists()).toBe(false)
  expect(wrapper.find('.lumen-icon').exists()).toBe(true)
})

it('fills the mosaic from earlier days when the newest day has one photo', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([
    { day: '2026-10-16', count: 1 },
    { day: '2026-10-02', count: 4 },
  ])
  vi.mocked(engine.days).mockImplementation((day) =>
    Promise.resolve([
      { day, items: (day === '2026-10-16' ? [9] : [8, 7, 6, 5]).map((id) => ({ id, t: 2, w: 1, h: 1 })) },
    ]),
  )
  wrapper = mountInApp(CoverMosaic, { props: album })
  await flushPromises()
  expect(wrapper.findAll('img').map((img) => img.attributes('src'))).toEqual([
    expect.stringContaining('/9'),
    expect.stringContaining('/8'),
    expect.stringContaining('/7'),
  ])
})

it('takes its photos away at once when blocks change, leaving the cover', async () => {
  setActivePinia(createPinia())
  wrapper = mountInApp(CoverMosaic, { props: album })
  await flushPromises()
  expect(wrapper.findAll('img')).toHaveLength(3)
  await showBlocksChanged()
  await nextTick()
  expect(wrapper.findAll('img')).toHaveLength(1)
})
