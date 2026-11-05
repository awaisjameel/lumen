import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { reactive } from 'vue'
import { engine, Filter } from '../bridge'
import type * as Bridge from '../bridge'
import type { Event, NamedPlace } from '../bridge'
import { showBlocksChanged } from '../shared/block'
import { deferred, mountInApp, recordNotifications } from '../shared/testing'
import CollectionHead from './CollectionHead.vue'
import { useLibrary } from './store'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    setEventName: vi.fn(),
    setEventPinned: vi.fn(),
    things: vi.fn(),
    albums: vi.fn(),
    topPlaces: vi.fn().mockResolvedValue([]),
    counts: vi.fn().mockResolvedValue({ photos: 0, videos: 0, favourites: 0, people: 0, trash: 0 }),
  },
}))
beforeEach(() => {
  vi.mocked(engine.setEventName).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setEventPinned).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.topPlaces).mockReset().mockResolvedValue([])
})
afterEach(() => vi.useRealTimers())
async function mountHead(nameOverride = '') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/albums', name: 'albums', component: { template: '<div />' } },
      { path: '/places', name: 'places', component: { template: '<div />' } },
      { path: '/places/:id', name: 'place', component: { template: '<div />' } },
    ],
  })
  await router.push('/albums')
  const props = reactive({
    kind: 'event' as const,
    id: 1,
    name: nameOverride || 'Weekend',
    count: 248,
    revision: 0,
    days: ['2026-10-04', '2026-10-03'],
    group: {
      id: 1,
      name: nameOverride || 'Weekend',
      nameOverride,
      count: 248,
      startAt: 0,
      endAt: 0,
      cover: 0,
      coverThumb: 0,
      placeId: 0,
      pinned: false,
    } satisfies Event,
  })
  const wrapper = mountInApp(CollectionHead, { props, plugins: [createPinia(), router] })
  return { wrapper, props }
}
it('renames inline with Enter and keeps Undo bound to the original event', async () => {
  const notices = recordNotifications()
  const { wrapper, props } = await mountHead()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('[aria-label="Collection name"]').setValue('  At the sea  ')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenCalledExactlyOnceWith(1, 'At the sea')
  props.id = 2
  notices.find('collection.name')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setEventName).toHaveBeenLastCalledWith(1, '')
  wrapper.unmount()
  notices.stop()
})
it('restores the exact custom event override when undoing a rename or Reset name', async () => {
  const notices = recordNotifications()
  const { wrapper } = await mountHead('Our weekend')
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('input').setValue('At the sea')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  notices.find('collection.name')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setEventName).toHaveBeenLastCalledWith(1, 'Our weekend')
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Reset name')
    ?.trigger('click')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenLastCalledWith(1, '')
  notices.find('collection.name')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setEventName).toHaveBeenLastCalledWith(1, 'Our weekend')
  wrapper.unmount()
  notices.stop()
})
it('keeps an invalid name and explains the limit without saving', async () => {
  const { wrapper } = await mountHead()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('input').setValue('x'.repeat(81))
  await wrapper.get('form').trigger('submit')
  expect(engine.setEventName).not.toHaveBeenCalled()
  expect(wrapper.get('[role="alert"]').text()).toContain('1 to 80')
  wrapper.unmount()
})
it('allows a blank event name to restore the generated name and Escape abandons editing', async () => {
  const { wrapper } = await mountHead()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('input').setValue(' ')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenCalledWith(1, '')
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('form').trigger('keydown', { key: 'Escape' })
  expect(wrapper.find('input').exists()).toBe(false)
  wrapper.unmount()
})
it('retains a failed rename draft for retry', async () => {
  vi.mocked(engine.setEventName).mockRejectedValueOnce(new Error('read only'))
  const { wrapper } = await mountHead()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('input').setValue('Retained')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('read only')
  expect(wrapper.get('input').element.value).toBe('Retained')
  wrapper.unmount()
})
it('says where most of the collection was taken after what it holds, and asks again as that changes', async () => {
  vi.useFakeTimers()
  vi.mocked(engine.topPlaces).mockResolvedValue([
    { id: 7, name: 'Karachi' },
    { id: 8, name: 'Skardu' },
  ])
  const { wrapper, props } = await mountHead()
  await flushPromises()
  expect(engine.topPlaces).toHaveBeenLastCalledWith({ filter: Filter.FilterAll, event: 1 }, expect.any(AbortSignal))
  expect(wrapper.text().replace(/\s+/g, ' ')).toMatch(/248 items · Karachi · Skardu/)
  expect(wrapper.findAll('[data-collection-place]').map((link) => link.attributes('href'))).toEqual([
    '/places/7',
    '/places/8',
  ])
  props.count = 249
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(engine.topPlaces).toHaveBeenCalledTimes(2)
  wrapper.unmount()
})
it('offers a kind’s own action as the primary button and Block as one that asks first', async () => {
  const { wrapper, props } = await mountHead()
  Object.assign(props, { kind: 'place', id: 7 })
  await flushPromises()
  const map = wrapper.findAll('a').find((b) => b.text() === 'Open on map')
  expect(map?.classes()).toContain('bg-primary')
  expect(wrapper.findAll('button').some((b) => b.text() === 'Block…')).toBe(true)
  wrapper.unmount()
})

it('coalesces same-count changes and indexing growth into one trailing metadata read', async () => {
  vi.useFakeTimers()
  vi.mocked(engine.topPlaces).mockResolvedValue([{ id: 7, name: 'Karachi' }])
  const { wrapper, props } = await mountHead()
  await flushPromises()
  for (let i = 0; i < 100; i++) {
    props.revision++
    if (i >= 50) props.count++
    await flushPromises()
  }
  expect(wrapper.find('[data-collection-place]').exists()).toBe(false)
  expect(engine.topPlaces).toHaveBeenCalledOnce()
  vi.mocked(engine.topPlaces).mockResolvedValue([{ id: 8, name: 'Lahore' }])
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(engine.topPlaces).toHaveBeenCalledTimes(2)
  expect(wrapper.get('[data-collection-place]').text()).toBe('Lahore')
  props.revision++
  await flushPromises()
  wrapper.unmount()
  await vi.advanceTimersByTimeAsync(3000)
  expect(engine.topPlaces).toHaveBeenCalledTimes(2)
})

it('reconciles correction and Undo metadata immediately without a new event subscription', async () => {
  vi.useFakeTimers()
  vi.mocked(engine.topPlaces).mockResolvedValue([{ id: 7, name: 'Karachi' }])
  const { wrapper } = await mountHead()
  await flushPromises()
  vi.mocked(engine.topPlaces).mockResolvedValue([{ id: 8, name: 'Lahore' }])
  await useLibrary().reconcileChanges()
  await flushPromises()
  expect(wrapper.get('[data-collection-place]').attributes('href')).toBe('/places/8')
  vi.mocked(engine.topPlaces).mockResolvedValue([{ id: 7, name: 'Karachi' }])
  await useLibrary().reconcileChanges()
  await flushPromises()
  expect(wrapper.get('[data-collection-place]').attributes('href')).toBe('/places/7')
  expect(engine.topPlaces).toHaveBeenCalledTimes(3)
  wrapper.unmount()
})

it('clears private metadata before a failed privacy read and offers an accessible retry', async () => {
  vi.mocked(engine.topPlaces).mockResolvedValue([{ id: 7, name: 'Karachi' }])
  const { wrapper } = await mountHead()
  await flushPromises()
  const pending = deferred<NamedPlace[]>()
  vi.mocked(engine.topPlaces).mockReturnValueOnce(pending.promise)
  const refreshing = showBlocksChanged()
  const rejected = expect(refreshing).rejects.toThrow('read failed')
  await flushPromises()
  expect(wrapper.find('[data-collection-place]').exists()).toBe(false)
  pending.reject(new Error('read failed'))
  await rejected
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('read failed')
  expect(wrapper.text()).not.toContain('Karachi')
  vi.mocked(engine.topPlaces).mockResolvedValue([{ id: 8, name: 'Lahore' }])
  await wrapper.get('[role="alert"] button').trigger('click')
  await flushPromises()
  expect(wrapper.get('[data-collection-place]').attributes('href')).toBe('/places/8')
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  wrapper.unmount()
})

it('cancels an older collection read and never applies it to the next identity', async () => {
  const older = deferred<NamedPlace[]>()
  vi.mocked(engine.topPlaces)
    .mockReturnValueOnce(older.promise)
    .mockResolvedValue([{ id: 8, name: 'Lahore' }])
  const { wrapper, props } = await mountHead()
  const signal = vi.mocked(engine.topPlaces).mock.calls[0]?.[1]
  props.id = 2
  await flushPromises()
  expect(signal?.aborted).toBe(true)
  expect(engine.topPlaces).toHaveBeenCalledOnce()
  older.resolve([{ id: 7, name: 'Karachi' }])
  await flushPromises()
  expect(engine.topPlaces).toHaveBeenCalledTimes(2)
  expect(engine.topPlaces).toHaveBeenLastCalledWith({ filter: Filter.FilterAll, event: 2 }, expect.any(AbortSignal))
  expect(wrapper.get('[data-collection-place]').text()).toBe('Lahore')
  expect(wrapper.text()).not.toContain('Karachi')
  wrapper.unmount()
})

it('retires pending metadata and queued reads when its header unmounts', async () => {
  const older = deferred<NamedPlace[]>()
  vi.mocked(engine.topPlaces).mockReturnValueOnce(older.promise)
  const { wrapper, props } = await mountHead()
  const signal = vi.mocked(engine.topPlaces).mock.calls[0]?.[1]
  props.id = 2
  await flushPromises()
  wrapper.unmount()
  older.resolve([{ id: 7, name: 'Karachi' }])
  await flushPromises()
  expect(signal?.aborted).toBe(true)
  expect(engine.topPlaces).toHaveBeenCalledOnce()
  await showBlocksChanged()
  expect(engine.topPlaces).toHaveBeenCalledOnce()
})
