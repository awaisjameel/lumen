import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { computed } from 'vue'
import { createPinia } from 'pinia'
import { undo } from './details/testing'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { BatchAction, engine } from '../bridge'
import type * as Bridge from '../bridge'
import { mountInApp, recordNotifications } from '../shared/testing'
import AlbumPhotoPicker from './AlbumPhotoPicker.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    timeline: vi.fn(),
    days: vi.fn(),
    selection: vi.fn(),
    applyBatch: vi.fn(),
    albums: vi.fn(),
    counts: vi.fn(),
  },
}))
vi.mock('@tanstack/vue-virtual', () => ({
  useVirtualizer: (options: { value: { count: number } }) =>
    computed(() => ({
      getTotalSize: () => options.value.count * 120,
      getVirtualItems: () =>
        Array.from({ length: options.value.count }, (_, index) => ({ index, key: index, start: index * 120 })),
    })),
}))
let notices: ReturnType<typeof recordNotifications>
afterEach(() => {
  for (const notice of notices.shown) notice.closed?.()
  notices.stop()
})
beforeEach(() => {
  notices = recordNotifications()
  vi.mocked(engine.albums).mockResolvedValue([])
  vi.mocked(engine.counts).mockResolvedValue({ photos: 2, videos: 0, favourites: 0, people: 0, trash: 0 })
  vi.mocked(engine.timeline).mockResolvedValue([{ day: '2026-10-01', count: 2 }])
  vi.mocked(engine.days).mockResolvedValue([
    {
      day: '2026-10-01',
      items: [
        { id: 1, w: 1, h: 1, t: 1 },
        { id: 2, w: 1, h: 1, t: 1 },
      ],
    },
  ])
  vi.mocked(engine.selection).mockResolvedValue({ ids: [], revision: 1 })
  vi.mocked(engine.applyBatch)
    .mockReset()
    .mockImplementation((ids) => Promise.resolve(ids.map((id) => ({ id, code: '', error: '', changed: true }))))
})

it('adds the checked photos to the named album without changing the library scope', async () => {
  const wrapper = mountInApp(AlbumPhotoPicker, {
    props: { open: true, albumId: 8, name: 'Summer' },
    plugins: [createPinia()],
  })
  await flushPromises()
  const body = new DOMWrapper(document.body)
  const add = body.findAll('button').find((button) => button.text() === 'Add selected photos')
  expect(add?.attributes('disabled')).toBeDefined()
  await body.get('[aria-label="Select item 1"]').trigger('click')
  await add?.trigger('click')
  await flushPromises()
  expect(engine.applyBatch).toHaveBeenCalledExactlyOnceWith([1], BatchAction.BatchAlbum, 8)
  expect(wrapper.findComponent(AlbumPhotoPicker).emitted('changed')).toHaveLength(1)
  wrapper.unmount()
})

it('retains failed items for retry after a partially successful add', async () => {
  vi.mocked(engine.applyBatch).mockResolvedValueOnce([
    { id: 1, code: '', error: '', changed: true },
    { id: 2, code: '', error: 'read only' },
  ])
  const wrapper = mountInApp(AlbumPhotoPicker, {
    props: { open: true, albumId: 8, name: 'Summer' },
    plugins: [createPinia()],
  })
  await flushPromises()
  const body = new DOMWrapper(document.body)
  await body.get('[aria-label="Select item 1"]').trigger('click')
  await body.get('[aria-label="Select item 2"]').trigger('click')
  const add = body.findAll('button').find((button) => button.text() === 'Add selected photos')
  await add?.trigger('click')
  await flushPromises()
  expect(body.get('[role="alert"]').text()).toContain('read only')
  expect(notices.shown.flatMap((notice) => notice.actions ?? []).some((action) => action.label === 'Undo')).toBe(true)
  expect(body.get('[aria-label="Select item 1"]').attributes('aria-pressed')).toBe('false')
  expect(body.get('[aria-label="Select item 2"]').attributes('aria-pressed')).toBe('true')
  await add?.trigger('click')
  await flushPromises()
  expect(engine.applyBatch).toHaveBeenLastCalledWith([2], BatchAction.BatchAlbum, 8)
  wrapper.unmount()
})

it('reconciles successful photo-picker inverses before reporting a refused row', async () => {
  const wrapper = mountInApp(AlbumPhotoPicker, {
    props: { open: true, albumId: 8, name: 'Summer' },
    plugins: [createPinia()],
  })
  await flushPromises()
  const body = new DOMWrapper(document.body)
  await body.get('[aria-label="Select item 1"]').trigger('click')
  await body.get('[aria-label="Select item 2"]').trigger('click')
  await body
    .findAll('button')
    .find((button) => button.text() === 'Add selected photos')
    ?.trigger('click')
  await flushPromises()
  vi.mocked(engine.applyBatch).mockResolvedValueOnce([
    { id: 1, code: '', error: '', changed: true },
    { id: 2, code: 'not_found', error: 'Unavailable asset' },
  ])
  await undo(notices)
  expect(wrapper.findComponent(AlbumPhotoPicker).emitted('changed')).toHaveLength(2)
  wrapper.unmount()
})
