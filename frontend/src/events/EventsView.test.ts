import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine, MAX_NAME_LEN, onLibraryChanged } from '../bridge'
import type * as Bridge from '../bridge'
import type { Changes, Event } from '../bridge'
import EventsView from './EventsView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { events: vi.fn(), setEventName: vi.fn() },
}))

const trip: Event = {
  id: 7,
  name: 'Lahore, 2 Jan 2024',
  startAt: 0,
  endAt: 0,
  count: 3,
  cover: 0,
  coverThumb: 0,
  placeId: 0,
  pinned: false,
}
let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
  vi.mocked(engine.events).mockReset().mockResolvedValue([trip])
  vi.mocked(engine.setEventName).mockReset().mockResolvedValue(undefined)
})
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})

async function mounted() {
  wrapper = mount(EventsView, {
    attachTo: document.body,
    global: { stubs: { SearchBox: true, RouterLink: { template: '<a><slot /></a>' } } },
  })
  await flushPromises()
  return wrapper
}

it('focuses the name to edit, and Escape puts focus back on the pencil', async () => {
  const w = await mounted()
  await w.get('button.icon-btn').trigger('click')
  await flushPromises()
  const input = w.get<HTMLInputElement>('form.rename input').element
  expect(document.activeElement).toBe(input)
  await w.get('form.rename').trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(w.find('form.rename').exists()).toBe(false)
  expect(document.activeElement).toBe(w.get('button.icon-btn').element)
})

it('keeps a rename failure on screen while the list reloads around it', async () => {
  let changed: ((c: Changes) => void) | undefined
  vi.mocked(onLibraryChanged).mockImplementation((cb) => {
    changed = cb
    return () => undefined
  })
  vi.mocked(engine.setEventName).mockRejectedValue(new Error('disk full'))
  const w = await mounted()
  await w.get('button.icon-btn').trigger('click')
  await flushPromises()
  await w.get('form.rename').trigger('submit')
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toContain('disk full')
  changed?.({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000) // the live reload
  await flushPromises()
  expect(engine.events).toHaveBeenCalledTimes(2)
  expect(w.get('[role="alert"]').text()).toContain('disk full')
})

it('validates emoji names by code point and still allows resetting an overlong draft', async () => {
  const w = await mounted()
  await w.get('button.icon-btn').trigger('click')
  await flushPromises()
  expect(w.get('form.rename input').attributes('maxlength')).toBeUndefined()
  await w.get('form.rename input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await w.get('form.rename').trigger('submit')
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toContain('80')
  expect(engine.setEventName).not.toHaveBeenCalled()
  await w.get('form.rename input').setValue('😀'.repeat(MAX_NAME_LEN))
  await w.get('form.rename').trigger('submit')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenCalledWith(trip.id, '😀'.repeat(MAX_NAME_LEN))
  await w.get('button.icon-btn').trigger('click')
  await flushPromises()
  await w.get('form.rename input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await w.findAll('form.rename button')[2]?.trigger('click')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenLastCalledWith(trip.id, '')
})

it('discards a rename draft when its event disappears and later returns', async () => {
  let changed: ((c: Changes) => void) | undefined
  vi.mocked(onLibraryChanged).mockImplementation((cb) => {
    changed = cb
    return () => undefined
  })
  const w = await mounted()
  await w.get('button.icon-btn').trigger('click')
  await w.get('form.rename input').setValue('Unsaved name')
  vi.mocked(engine.events).mockResolvedValue([])
  changed?.({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(w.find('form.rename').exists()).toBe(false)
  vi.mocked(engine.events).mockResolvedValue([trip])
  changed?.({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(w.find('form.rename').exists()).toBe(false)
  await w.get('button.icon-btn').trigger('click')
  expect(w.get<HTMLInputElement>('form.rename input').element.value).toBe(trip.name)
})
