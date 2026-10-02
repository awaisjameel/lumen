import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { DateSource, engine, Kind, Playback, videosAvailable } from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail } from '../bridge'
import { useLibrary } from './store'
import type { ViewerSource } from './store'
import Viewer from './Viewer.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { asset: vi.fn() },
  videosAvailable: vi.fn(() => true),
}))

const detail: AssetDetail = {
  id: 1,
  kind: Kind.KindVideo,
  takenAt: 0,
  dateFrom: DateSource.DateFromFile,
  width: 640,
  height: 480,
  camera: '',
  lat: null,
  lon: null,
  placeId: 0,
  favourite: false,
  paths: [],
  size: 100,
  ext: '.mp4',
  webNative: false,
  video: { duration: 60000, codec: 'h264', audio: 'aac', play: Playback.PlayDirect },
}
let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  lib.days = new Map([['results', [{ id: 1, w: 640, h: 480, t: 1, v: true, at: 10000 }]]])
  lib.viewer = { day: 'results', id: 1 }
  vi.mocked(engine.asset).mockResolvedValue(detail)
})
afterEach(() => wrapper?.unmount())

it('shows the video without a still image covering playback', async () => {
  wrapper = mount(Viewer, { global: { stubs: { teleport: true } } })
  await flushPromises()
  expect(wrapper.find('video').exists()).toBe(true)
  expect(wrapper.find('.media img').exists()).toBe(false)
})

it('says so when videos cannot play at all', async () => {
  vi.mocked(videosAvailable).mockReturnValue(false)
  wrapper = mount(Viewer, { global: { stubs: { teleport: true } } })
  await flushPromises()
  expect(wrapper.find('video').exists()).toBe(false)
  expect(wrapper.get('[role="alert"]').text()).toContain('couldn’t start its video player')
  vi.mocked(videosAvailable).mockReturnValue(true)
})

it('uses the absolute media clock for direct playback from a search moment', async () => {
  wrapper = mount(Viewer, { global: { stubs: { teleport: true } } })
  await flushPromises()
  const video = wrapper.get('video')
  await video.trigger('loadedmetadata')
  expect(video.element.currentTime).toBe(10)
  video.element.currentTime = 12
  await video.trigger('timeupdate')
  expect(wrapper.get<HTMLInputElement>('input[type="range"]').element.value).toBe('12000')
})

it('adds the stream offset only for converted playback', async () => {
  vi.mocked(engine.asset).mockResolvedValue({
    ...detail,
    video: { duration: 60000, codec: 'mpeg4', audio: '', play: Playback.PlayConvert },
  })
  wrapper = mount(Viewer, { global: { stubs: { teleport: true } } })
  await flushPromises()
  const video = wrapper.get('video')
  video.element.currentTime = 2
  await video.trigger('timeupdate')
  expect(wrapper.get<HTMLInputElement>('input[type="range"]').element.value).toBe('12000')
  expect(video.attributes('src')).toContain('at=10000')
})

it('does not reopen a closed viewer when neighbour loading finishes', async () => {
  const lib = useLibrary()
  let finish!: (value: { day: string; id: number }) => void
  vi.spyOn(lib, 'neighbour').mockReturnValue(
    new Promise((resolve) => {
      finish = resolve
    }),
  )
  wrapper = mount(Viewer, { global: { stubs: { teleport: true } } })
  await flushPromises()
  await wrapper.get('.next').trigger('click')
  lib.viewer = null
  finish({ day: 'results', id: 2 })
  await flushPromises()
  expect(lib.viewer).toBeNull()
})

it('ignores details from an earlier visit to the same asset', async () => {
  let finish!: (value: AssetDetail) => void
  vi.mocked(engine.asset).mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
  wrapper = mount(Viewer, { global: { stubs: { teleport: true } } })
  const lib = useLibrary()
  lib.viewer = { day: 'results', id: 2 }
  await flushPromises()
  lib.viewer = { day: 'results', id: 1 }
  await flushPromises()
  expect(wrapper.find('video').exists()).toBe(true)
  finish({ ...detail, video: null, webNative: true })
  await flushPromises()
  expect(wrapper.find('video').exists()).toBe(true)
})

it('leaves the arrow keys to the seek slider, and Escape to an open menu', async () => {
  const lib = useLibrary()
  const neighbour = vi.spyOn(lib, 'neighbour').mockResolvedValue(null)
  wrapper = mount(Viewer, { global: { stubs: { teleport: true } }, attachTo: document.body })
  await flushPromises()
  const slider = wrapper.get('input[type="range"]').element
  slider.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
  expect(neighbour).not.toHaveBeenCalled()

  // A control that handles Escape itself (a picker closing its menu) keeps the viewer open.
  const picker = document.createElement('input')
  picker.addEventListener('keydown', (e) => e.stopPropagation())
  wrapper.element.appendChild(picker)
  picker.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  expect(lib.viewer).not.toBeNull()

  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }))
  expect(neighbour).toHaveBeenCalledOnce()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
  expect(lib.viewer).toBeNull()
})

function fakeSource(more: Partial<ViewerSource['actions']> = {}): ViewerSource {
  return {
    id: 5,
    item: { id: 5, w: 4, h: 3, t: 1 },
    sel: null,
    step: vi.fn().mockResolvedValue(undefined),
    close: vi.fn(),
    actions: { details: false, ...more },
  }
}

it('shows the photo of the source it is given, not the library’s', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, id: 5, video: null, webNative: true })
  wrapper = mount(Viewer, { props: { source: fakeSource() }, global: { stubs: { teleport: true } } })
  await flushPromises()
  expect(engine.asset).toHaveBeenCalledExactlyOnceWith(5)
  expect(wrapper.get('img.preview').attributes('src')).toContain('/media/thumb/5/')
})

it('moves and closes through its source', async () => {
  const source = fakeSource()
  wrapper = mount(Viewer, { props: { source }, global: { stubs: { teleport: true } } })
  await flushPromises()
  await wrapper.get('.next').trigger('click')
  await wrapper.get('.prev').trigger('click')
  expect(vi.mocked(source.step).mock.calls).toEqual([[1], [-1]])
  await wrapper.get('button[aria-label="Close"]').trigger('click')
  expect(source.close).toHaveBeenCalledOnce()
  expect(useLibrary().viewer).toEqual({ day: 'results', id: 1 }) // the library's own viewer is not touched
})

it('offers only the actions its source gives, and no details when it says so', async () => {
  const moveToTrash = vi.fn()
  wrapper = mount(Viewer, { props: { source: fakeSource({ moveToTrash }) }, global: { stubs: { teleport: true } } })
  await flushPromises()
  const labels = wrapper.findAll('.bar button').map((b) => b.text() || b.attributes('aria-label'))
  expect(labels).toEqual(['Close', 'Move to Trash'])
  await wrapper.get('button[aria-label="Move to Trash"]').trigger('click')
  expect(moveToTrash).toHaveBeenCalledOnce()
})

it('ignores the favourite and info keys that its source does not offer', async () => {
  const lib = useLibrary()
  const toggle = vi.spyOn(lib, 'toggleFavourite')
  wrapper = mount(Viewer, {
    props: { source: fakeSource() },
    global: { stubs: { teleport: true } },
    attachTo: document.body,
  })
  await flushPromises()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'f' }))
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'i' }))
  expect(toggle).not.toHaveBeenCalled()
  expect(lib.showDetails).toBe(false)
})

it('offers the favourite and info keys when its source does', async () => {
  const favourite = vi.fn()
  const source = fakeSource({ favourite, details: true })
  wrapper = mount(Viewer, { props: { source }, global: { stubs: { teleport: true } }, attachTo: document.body })
  await flushPromises()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'f' }))
  expect(favourite).toHaveBeenCalledOnce()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'i' }))
  expect(useLibrary().showDetails).toBe(true)
})

it('shows no details panel for a source without details, whatever the library has open', async () => {
  useLibrary().showDetails = true
  wrapper = mount(Viewer, { props: { source: fakeSource() }, global: { stubs: { teleport: true } } })
  await flushPromises()
  expect(wrapper.find('aside').exists()).toBe(false)
})

it('tells its source it is being watched as a video plays', async () => {
  const touch = vi.fn()
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, id: 5 })
  wrapper = mount(Viewer, { props: { source: { ...fakeSource(), touch } }, global: { stubs: { teleport: true } } })
  await flushPromises()
  await wrapper.get('video').trigger('timeupdate')
  expect(touch).toHaveBeenCalled()
})
