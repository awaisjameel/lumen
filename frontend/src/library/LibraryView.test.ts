import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { Device, engine, Filter, onLibraryChanged, WaitReason } from '../bridge'
import type * as Bridge from '../bridge'
import type { Changes, Event, Progress } from '../bridge'
import { bytes } from '../shared/format'
import { t } from '../shared/i18n'
import { mountInApp } from '../shared/testing'
import LibraryView from './LibraryView.vue'
import { useLibrary } from './store'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    timeline: vi.fn<typeof Bridge.engine.timeline>().mockResolvedValue([]),
    event: vi.fn<typeof Bridge.engine.event>(),
    days: vi.fn<typeof Bridge.engine.days>().mockResolvedValue([]),
    trashSelection: vi.fn<typeof Bridge.engine.trashSelection>(),
    focus: vi.fn<typeof Bridge.engine.focus>().mockResolvedValue(undefined),
  },
  onLibraryChanged: vi.fn<typeof Bridge.onLibraryChanged>(),
}))

const listeners = new Set<(changes: Changes) => void>()
function changed(change: Changes) {
  for (const callback of listeners) callback(change)
}
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
  listeners.clear()
  vi.useFakeTimers()
  vi.mocked(engine.event).mockReset()
  vi.mocked(onLibraryChanged).mockImplementation((callback) => {
    listeners.add(callback)
    return () => {
      listeners.delete(callback)
    }
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
  vi.restoreAllMocks()
})

async function mountedTimeline() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/events/:id', name: 'event', component: LibraryView },
      { path: '/events', name: 'events', component: { template: '<div />' } },
    ],
  })
  await router.push('/events/1')
  const wrapper = mountInApp(LibraryView, { plugins: [createPinia(), router], stubs: { SearchBox: true } })
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
  const wrapper = mountInApp(LibraryView, { plugins: [createPinia(), router], stubs: { SearchBox: true } })
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

it('puts the Trash actions and its files on disk in the header, with no photo search', async () => {
  vi.mocked(engine.timeline).mockResolvedValueOnce([{ day: '2021-01-23', count: 3 }])
  vi.mocked(engine.trashSelection).mockResolvedValue({ assets: [1, 2, 3], files: 5 })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/trash', name: 'trash', component: LibraryView, meta: { filter: Filter.FilterTrash } }],
  })
  await router.push('/trash')
  const wrapper = mountInApp(LibraryView, { plugins: [createPinia(), router], stubs: { SearchBox: true } })
  await flushPromises()
  const header = wrapper.get('header')
  expect(header.text()).toContain('3 items · 5 files on disk')
  expect(header.find('[data-trash-restore-all]').exists()).toBe(true)
  expect(wrapper.find('search-box-stub').exists()).toBe(false)
  expect(wrapper.get('[data-trash-notice]').text()).toContain(t('trash.summary', { n: 3 }))
  // Its items are files to restore or delete: no view bar, no Details toggle.
  expect(wrapper.find('[role="toolbar"]').exists()).toBe(false)
  expect(header.find(`[aria-label="${t('library.info')}"]`).exists()).toBe(false)
  wrapper.unmount()
})

/** A ResizeObserver that reports one fixed size, so the virtual grid draws rows. */
class SizedResizeObserver implements ResizeObserver {
  constructor(private readonly callback: ResizeObserverCallback) {}
  observe(target: Element) {
    const size: ResizeObserverSize = { inlineSize: 900, blockSize: 600 }
    const entry = {
      target,
      contentRect: new DOMRect(0, 0, 900, 600),
      borderBoxSize: [size],
      contentBoxSize: [size],
      devicePixelContentBoxSize: [size],
    }
    this.callback([entry], this)
  }
  unobserve = vi.fn()
  disconnect = vi.fn()
}

it('groups the Trash by the day items were moved there and names each by its file', async () => {
  vi.stubGlobal('ResizeObserver', SizedResizeObserver)
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(900)
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(600)
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(900)
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(600)
  vi.mocked(engine.timeline).mockResolvedValueOnce([{ day: '2026-10-15', count: 2 }])
  vi.mocked(engine.days).mockResolvedValueOnce([
    {
      day: '2026-10-15',
      items: [
        { id: 1, w: 4, h: 3, t: 1, name: 'IMG_2469.JPG', size: 6_500_000, files: 1 },
        { id: 2, w: 4, h: 3, t: 1, v: true, name: 'VID_2490.MP4', size: 2048, files: 2 },
      ],
    },
  ])
  vi.mocked(engine.trashSelection).mockResolvedValue({ assets: [1, 2], files: 3 })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/trash', name: 'trash', component: LibraryView, meta: { filter: Filter.FilterTrash } }],
  })
  await router.push('/trash')
  const wrapper = mountInApp(LibraryView, {
    plugins: [createPinia(), router],
    stubs: { SearchBox: true, VideoThumb: true, LibraryThumb: true },
  })
  await flushPromises()
  await vi.advanceTimersByTimeAsync(50)
  await flushPromises()
  expect(wrapper.get('.day-header h2').text()).toBe(t('trash.movedOn', { day: '15 October 2026' }))
  const captions = wrapper.findAll('[data-trash-caption]').map((c) => c.text())
  expect(captions[0]).toContain('IMG_2469.JPG')
  expect(captions[0]).toContain('6.2 MB · Photo')
  expect(captions[1]).toContain('VID_2490.MP4')
  expect(captions[1]).toContain(`${bytes(2048)} · Video · 2 files`)
  expect(wrapper.get('[role="option"]').attributes('aria-label')).toBe(
    t('trash.cell', { name: 'IMG_2469.JPG', day: '15 October 2026' }),
  )
  // The day's selection is offered in words, and taken back the same way.
  const lib = useLibrary()
  await wrapper.get('[data-trash-select-day]').trigger('click')
  await flushPromises()
  expect([...lib.selectedIDs]).toEqual([1, 2])
  expect(wrapper.get('[data-trash-select-day]').text()).toBe(t('selection.clear'))
  // The details button shows that item in the details panel.
  await wrapper.findAll('[data-trash-caption] button')[1]?.trigger('click')
  await flushPromises()
  expect(lib.selected?.id).toBe(2)
  expect(lib.showDetails).toBe(true)
  expect(engine.focus).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('rings the one of several selected items that the details panel shows', async () => {
  vi.stubGlobal('ResizeObserver', SizedResizeObserver)
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(900)
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(600)
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(900)
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(600)
  vi.mocked(engine.timeline).mockResolvedValueOnce([{ day: '2026-10-03', count: 3 }])
  vi.mocked(engine.days).mockResolvedValueOnce([
    { day: '2026-10-03', items: [1, 2, 3].map((id) => ({ id, w: 4, h: 3, t: 1 })) },
  ])
  const { wrapper, lib } = await mountedLibrary()
  await vi.advanceTimersByTimeAsync(50)
  await flushPromises()
  const shown = () => wrapper.findAll('.cell.shown').map((cell) => cell.attributes('id'))
  lib.selected = { day: '2026-10-03', id: 2 }
  await flushPromises()
  // One item selected is ringed by its selection alone.
  expect(shown()).toEqual([])
  lib.selectVisible([1, 2])
  lib.selected = { day: '2026-10-03', id: 2 }
  await flushPromises()
  expect(shown()).toEqual(['cell-2'])
  wrapper.unmount()
})

it('uses the Photos title and passes the photos filter to the timeline', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', name: 'library', component: LibraryView, meta: { filter: Filter.FilterPhotos } }],
  })
  await router.push('/')
  const wrapper = mountInApp(LibraryView, { plugins: [createPinia(), router], stubs: { SearchBox: true } })
  await flushPromises()
  expect(wrapper.get('h1').text()).toBe('Photos')
  expect(engine.timeline).toHaveBeenCalledWith(
    expect.objectContaining({ filter: Filter.FilterPhotos }),
    expect.anything(),
  )
  wrapper.unmount()
})

it('leaves grid shortcuts to an open overlay and ignores composing key events', async () => {
  vi.mocked(engine.event).mockResolvedValue(event(1, 'Event'))
  const { wrapper } = await mountedTimeline()
  const lib = useLibrary()
  const grid = wrapper.get('[role="listbox"]')
  const dialog = document.createElement('div')
  dialog.setAttribute('role', 'dialog')
  dialog.dataset.state = 'open'
  document.body.append(dialog)
  await grid.trigger('keydown', { key: 'i' })
  expect(lib.showDetails).toBe(false)
  lib.selected = { day: '2026-01-01', id: 1 }
  await grid.trigger('keydown', { key: 'Escape' })
  expect(lib.selected).toEqual({ day: '2026-01-01', id: 1 })
  dialog.remove()
  await grid.trigger('keydown', { key: 'Escape', isComposing: true })
  expect(lib.selected).toEqual({ day: '2026-01-01', id: 1 })
  await grid.trigger('keydown', { key: 'Escape' })
  expect(lib.selected).toBeNull()
  wrapper.unmount()
})

it('says when days could not be loaded and offers to try again', async () => {
  vi.mocked(engine.event).mockResolvedValue(event(1, 'Event'))
  const { wrapper } = await mountedTimeline()
  const lib = useLibrary()
  const retry = vi.spyOn(lib, 'retryDays')
  lib.daysError = 'Something went wrong: disk busy'
  await flushPromises()
  const alert = wrapper.get('[role="alert"]')
  expect(alert.text()).toContain('Some photos couldn’t be loaded.')
  expect(alert.text()).toContain('disk busy')
  await alert.get('button').trigger('click')
  expect(retry).toHaveBeenCalledOnce()
  wrapper.unmount()
})

it('says it is looking while the first scan runs, and that nothing was found once it ends', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', name: 'library', component: LibraryView, meta: { filter: Filter.FilterAll } }],
  })
  await router.push('/')
  const pinia = createPinia()
  const wrapper = mountInApp(LibraryView, { plugins: [pinia, router], stubs: { SearchBox: true } })
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

async function mountedLibrary(path = '/') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'library', component: LibraryView },
      { path: '/videos', name: 'videos', component: LibraryView },
      { path: '/settings', name: 'settings', component: { template: '<div />' } },
      { path: '/compression', name: 'compression', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  const wrapper = mountInApp(LibraryView, { plugins: [createPinia(), router], stubs: { SearchBox: true } })
  await flushPromises()
  return { wrapper, router, lib: useLibrary() }
}

it('keeps view controls below the search header and hides the dock until selection', async () => {
  const { wrapper, lib } = await mountedLibrary()
  expect(wrapper.get('header').find('search-box-stub').exists()).toBe(true)
  expect(wrapper.get('header').find('[role="radiogroup"]').exists()).toBe(false)
  expect(wrapper.find('[data-command="library.trashSelection"]').exists()).toBe(false)
  lib.selectVisible([1, 2])
  await flushPromises()
  expect(wrapper.get('[data-command="library.trashSelection"]').text()).toContain('Trash')
  expect(wrapper.find('[data-command="library.exportSelection"]').exists()).toBe(true)
  await wrapper.get('[aria-label="Clear selection"]').trigger('click')
  expect(lib.selectedIDs.size).toBe(0)
  wrapper.unmount()
})

it('hides Videos when disabled and clears selection when the media filter changes', async () => {
  const { wrapper, lib, router } = await mountedLibrary()
  expect(wrapper.findAll('[role="radio"]').map((button) => button.text())).toEqual(['All', 'Photos'])
  lib.settings.videos = true
  lib.selectVisible([1])
  await flushPromises()
  await wrapper
    .findAll('[role="radio"]')
    .find((button) => button.text() === 'Photos')
    ?.trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.query.media).toBe('photos')
  expect(lib.selectedIDs.size).toBe(0)
  expect(engine.timeline).toHaveBeenLastCalledWith(
    expect.objectContaining({ filter: Filter.FilterPhotos }),
    expect.anything(),
  )
  wrapper.unmount()
})

it('opens the legacy Videos route with the Videos filter selected', async () => {
  const { wrapper, lib } = await mountedLibrary('/videos')
  lib.settings.videos = true
  await flushPromises()
  expect(
    wrapper
      .findAll('[role="radio"]')
      .find((button) => button.text() === 'Videos')
      ?.attributes('aria-checked'),
  ).toBe('true')
  expect(engine.timeline).toHaveBeenLastCalledWith(
    expect.objectContaining({ filter: Filter.FilterVideos }),
    expect.anything(),
  )
  wrapper.unmount()
})

it('counts photos and videos in the caption, leaving out videos the library has none of', async () => {
  const { wrapper, lib } = await mountedLibrary('/')
  lib.counts = { photos: 1200, videos: 0, favourites: 0, people: 0, trash: 0 }
  lib.countsLoaded = true
  await flushPromises()
  expect(wrapper.get('[aria-live="polite"]').text()).toBe('1,200 photos')
  lib.counts = { photos: 1200, videos: 3, favourites: 0, people: 0, trash: 0 }
  await flushPromises()
  expect(wrapper.get('[aria-live="polite"]').text()).toBe('1,200 photos · 3 videos')
  wrapper.unmount()
})

it('shows a live Photos caption using the filtered timeline total', async () => {
  const { wrapper, lib } = await mountedLibrary('/?media=photos')
  lib.buckets = [{ day: '2026-10-01', count: 248 }]
  await flushPromises()
  expect(wrapper.get('[aria-live="polite"]').text()).toBe('248 photos')
  wrapper.unmount()
})
