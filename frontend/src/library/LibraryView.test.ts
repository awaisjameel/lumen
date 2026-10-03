import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { Device, engine, Filter, onLibraryChanged, WaitReason } from '../bridge'
import type * as Bridge from '../bridge'
import type { Changes, Event, Progress } from '../bridge'
import LibraryView from './LibraryView.vue'
import { useLibrary } from './store'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    timeline: vi.fn<typeof Bridge.engine.timeline>().mockResolvedValue([]),
    event: vi.fn<typeof Bridge.engine.event>(),
  },
  onLibraryChanged: vi.fn<typeof Bridge.onLibraryChanged>(),
}))

let changed!: (changes: Changes) => void
const progressIdle: Progress = {
  scanning: false,
  scanPath: '',
  scanSeen: 0,
  total: 0,
  metadata: 0,
  thumbnail: 0,
  embedding: 0,
  faces: 0,
  videos: 0,
  ocr: 0,
  working: { metadata: false, thumbnail: false, embedding: false, faces: false, videos: false, ocr: false },
  errors: 0,
  skipped: 0,
  paused: false,
  onBattery: false,
  waiting: WaitReason.WaitNone,
  ai: { device: Device.DeviceCPU, error: '', facesError: '' },
}
const event = (id: number, name: string): Event => ({
  id,
  name,
  startAt: 0,
  endAt: 0,
  count: 2,
  cover: 0,
  coverThumb: 0,
  placeId: 0,
  pinned: false,
})

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers()
  vi.mocked(engine.event).mockReset()
  vi.mocked(onLibraryChanged).mockImplementation((callback) => {
    changed = callback
    return vi.fn()
  })
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe = vi.fn()
      unobserve = vi.fn()
      disconnect = vi.fn()
    },
  )
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function mountedTimeline() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/events/:id', name: 'event', component: LibraryView }],
  })
  await router.push('/events/1')
  const wrapper = mount(LibraryView, {
    global: { plugins: [createPinia(), router], stubs: { SearchBox: true } },
  })
  await flushPromises()
  return { wrapper, router }
}

it('refreshes an event heading after relevant library changes', async () => {
  vi.mocked(engine.event).mockResolvedValueOnce(event(1, 'Before')).mockResolvedValueOnce(event(1, 'After'))
  const { wrapper } = await mountedTimeline()
  expect(wrapper.get('h1').text()).toBe('Before')
  changed({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(wrapper.get('h1').text()).toBe('After')
  expect(engine.event).toHaveBeenCalledTimes(2)
  wrapper.unmount()
})

it('does not apply a previous event title after the route changes', async () => {
  let resolve!: (value: Event) => void
  vi.mocked(engine.event)
    .mockReturnValueOnce(new Promise((done) => (resolve = done)))
    .mockResolvedValueOnce(event(2, 'Second event'))
  const { wrapper, router } = await mountedTimeline()
  await router.push('/events/2')
  resolve(event(1, 'Old event'))
  await flushPromises()
  expect(wrapper.get('h1').text()).toBe('Second event')
  expect(engine.event).toHaveBeenNthCalledWith(2, 2)
  wrapper.unmount()
})

it('does not start a queued title read after unmount', async () => {
  let resolve!: (value: Event) => void
  vi.mocked(engine.event).mockReturnValueOnce(new Promise((done) => (resolve = done)))
  const { wrapper } = await mountedTimeline()
  changed({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  wrapper.unmount()
  resolve(event(1, 'Late event'))
  await flushPromises()
  expect(engine.event).toHaveBeenCalledOnce()
})

it('shows the empty Trash with its own message and no toolbar', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/trash', name: 'trash', component: LibraryView, meta: { filter: Filter.FilterTrash } }],
  })
  await router.push('/trash')
  const wrapper = mount(LibraryView, {
    global: { plugins: [createPinia(), router], stubs: { SearchBox: true } },
  })
  await flushPromises()
  expect(wrapper.get('h1').text()).toBe('Trash')
  expect(wrapper.text()).toContain('The Trash is empty')
  expect(wrapper.text()).not.toContain('Restore all')
  expect(engine.timeline).toHaveBeenCalledWith(
    expect.objectContaining({ filter: Filter.FilterTrash }),
    expect.anything(),
  )
  wrapper.unmount()
})

it('says it is looking while the first scan runs, and that nothing was found once it ends', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', name: 'library', component: LibraryView, meta: { filter: Filter.FilterAll } }],
  })
  await router.push('/')
  const pinia = createPinia()
  const wrapper = mount(LibraryView, { global: { plugins: [pinia, router], stubs: { SearchBox: true } } })
  await flushPromises()
  const lib = useLibrary(pinia)
  lib.progress = { ...progressIdle, scanning: true }
  await flushPromises()
  expect(wrapper.text()).toContain('Looking for photos…')
  expect(wrapper.text()).toContain('Your photos and videos appear here as Lumen finds them.')
  lib.progress = progressIdle
  await flushPromises()
  expect(wrapper.text()).toContain('No photos here yet')
  expect(wrapper.text()).toContain('Add a folder in Settings')
  wrapper.unmount()
})
