import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, getActivePinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import ConfirmDialog from '../shared/ConfirmDialog.vue'
import { confirm, question } from '../shared/confirm'
import { mountInApp } from '../shared/testing'
import { commandById, runCommand } from '../shared/commands'
import { DateSource, engine, Kind, Playback, videosAvailable } from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail } from '../bridge'
import { useLibrary } from './store'
import type { ViewerSource } from './store'
import Viewer from './Viewer.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { asset: vi.fn(), albums: vi.fn().mockResolvedValue([]) },
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
  caps: { preview: true, original: false, render: false, directPlay: true, convertPlay: true },
  video: { duration: 60000, codec: 'h264', audio: 'aac', play: Playback.PlayDirect },
}
let wrapper: VueWrapper | undefined
const view = () => new DOMWrapper(document.body)

function getTestPinia() {
  const pinia = getActivePinia()
  if (!pinia) throw new Error('No active test Pinia')
  return pinia
}

function mountViewer(source?: ViewerSource) {
  return mountInApp(Viewer, { props: source ? { source } : {}, plugins: [getTestPinia()] })
}

beforeEach(() => {
  // Reka UI measures the slider thumb with an observer, which jsdom does not provide.
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe = vi.fn()
      unobserve = vi.fn()
      disconnect = vi.fn()
    },
  )
  setActivePinia(createPinia())
  const lib = useLibrary()
  lib.days = new Map([['results', [{ id: 1, w: 640, h: 480, t: 1, v: true, at: 10000 }]]])
  lib.viewer = { day: 'results', id: 1 }
  vi.mocked(engine.asset).mockResolvedValue(detail)
})
afterEach(() => {
  question.value?.answer(false)
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})

it('shows the video without a still image covering playback', async () => {
  wrapper = mountViewer()
  await flushPromises()
  expect(view().find('video').exists()).toBe(true)
  expect(view().find('.media img').exists()).toBe(false)
})

it('says so when videos cannot play at all', async () => {
  vi.mocked(videosAvailable).mockReturnValue(false)
  wrapper = mountViewer()
  await flushPromises()
  expect(view().find('video').exists()).toBe(false)
  expect(view().get('[role="alert"]').text()).toContain('couldn’t start its video player')
  vi.mocked(videosAvailable).mockReturnValue(true)
})

it('uses the absolute media clock for direct playback from a search moment', async () => {
  wrapper = mountViewer()
  await flushPromises()
  const video = view().get<HTMLVideoElement>('video')
  await video.trigger('loadedmetadata')
  expect(video.element.currentTime).toBe(10)
  video.element.currentTime = 12
  await video.trigger('timeupdate')
  expect(view().get('[role="slider"]').attributes('aria-valuenow')).toBe('12000')
})

it('adds the stream offset only for converted playback', async () => {
  vi.mocked(engine.asset).mockResolvedValue({
    ...detail,
    video: { duration: 60000, codec: 'mpeg4', audio: '', play: Playback.PlayConvert },
  })
  wrapper = mountViewer()
  await flushPromises()
  const video = view().get<HTMLVideoElement>('video')
  video.element.currentTime = 2
  await video.trigger('timeupdate')
  expect(view().get('[role="slider"]').attributes('aria-valuenow')).toBe('12000')
  expect(video.attributes('src')).toContain('at=10000')
})

it('does not reopen a closed viewer when neighbour loading finishes', async () => {
  const lib = useLibrary()
  const pending = deferred<{ day: string; id: number }>()
  vi.spyOn(lib, 'neighbour').mockReturnValue(pending.promise)
  wrapper = mountViewer()
  await flushPromises()
  await view().get('.next').trigger('click')
  lib.viewer = null
  pending.resolve({ day: 'results', id: 2 })
  await flushPromises()
  expect(lib.viewer).toBeNull()
})

it('ignores details from an earlier visit to the same asset', async () => {
  const pending = deferred<AssetDetail>()
  vi.mocked(engine.asset).mockReturnValueOnce(pending.promise)
  wrapper = mountViewer()
  const lib = useLibrary()
  lib.viewer = { day: 'results', id: 2 }
  await flushPromises()
  lib.viewer = { day: 'results', id: 1 }
  await flushPromises()
  expect(view().find('video').exists()).toBe(true)
  pending.resolve({
    ...detail,
    video: null,
    caps: { preview: true, original: true, render: false, directPlay: false, convertPlay: false },
  })
  await flushPromises()
  expect(view().find('video').exists()).toBe(true)
})

it('leaves the arrow keys to the seek slider, and Escape to an open menu', async () => {
  const lib = useLibrary()
  const neighbour = vi.spyOn(lib, 'neighbour').mockResolvedValue(null)
  wrapper = mountViewer()
  await flushPromises()
  const slider = view().get('[role="slider"]').element
  slider.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
  expect(neighbour).not.toHaveBeenCalled()

  // A control that handles Escape itself (a picker closing its menu) keeps the viewer open.
  const picker = document.createElement('input')
  picker.addEventListener('keydown', (e) => e.stopPropagation())
  view().get('.viewer').element.appendChild(picker)
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
  vi.mocked(engine.asset).mockResolvedValue({
    ...detail,
    id: 5,
    video: null,
    caps: { preview: true, original: true, render: false, directPlay: false, convertPlay: false },
  })
  wrapper = mountViewer(fakeSource())
  await flushPromises()
  expect(engine.asset).toHaveBeenCalledExactlyOnceWith(5)
  expect(view().get('img.preview').attributes('src')).toContain('/media/thumb/5/')
})

it('moves and closes through its source', async () => {
  const source = fakeSource()
  wrapper = mountViewer(source)
  await flushPromises()
  await view().get('.next').trigger('click')
  await view().get('.prev').trigger('click')
  expect(vi.mocked(source.step).mock.calls).toEqual([[1], [-1]])
  await view().get('button[aria-label="Close"]').trigger('click')
  expect(source.close).toHaveBeenCalledOnce()
  expect(useLibrary().viewer).toEqual({ day: 'results', id: 1 }) // the library's own viewer is not touched
})

it('offers only the actions its source gives, and no details when it says so', async () => {
  const moveToTrash = vi.fn()
  wrapper = mountViewer(fakeSource({ moveToTrash }))
  await flushPromises()
  const labels = view()
    .findAll('.bar button')
    .map((b) => b.text() || b.attributes('aria-label'))
  expect(labels).toEqual(['Close', 'Move to Trash'])
  await view().get('button[aria-label="Move to Trash"]').trigger('click')
  expect(moveToTrash).toHaveBeenCalledOnce()
})

it('ignores the favourite and info keys that its source does not offer', async () => {
  const lib = useLibrary()
  const toggle = vi.spyOn(lib, 'toggleFavourite')
  wrapper = mountViewer(fakeSource())
  await flushPromises()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'f' }))
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'i' }))
  expect(toggle).not.toHaveBeenCalled()
  expect(lib.showDetails).toBe(false)
})

it('offers the favourite and info keys when its source does', async () => {
  const favourite = vi.fn()
  const source = fakeSource({ favourite, details: true })
  wrapper = mountViewer(source)
  await flushPromises()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'f' }))
  expect(favourite).toHaveBeenCalledOnce()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'i' }))
  expect(useLibrary().showDetails).toBe(true)
})

it('shows no details panel for a source without details, whatever the library has open', async () => {
  useLibrary().showDetails = true
  wrapper = mountViewer(fakeSource())
  await flushPromises()
  expect(view().find('aside').exists()).toBe(false)
})

it('tells its source it is being watched as a video plays', async () => {
  const touch = vi.fn()
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, id: 5 })
  wrapper = mountViewer({ ...fakeSource(), touch })
  await flushPromises()
  await view().get<HTMLVideoElement>('video').trigger('timeupdate')
  expect(touch).toHaveBeenCalled()
})

function deferred<T>() {
  let complete: ((value: T) => void) | undefined
  const promise = new Promise<T>((resolve) => {
    complete = resolve
  })
  return {
    promise,
    resolve(value: T) {
      if (!complete) throw new Error('Deferred promise was not initialised')
      complete(value)
    },
  }
}

it('loads the original only when the capability permits it and keeps the preview until it is ready', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, id: 5, video: null, caps: { ...detail.caps, original: true } })
  wrapper = mountViewer(fakeSource())
  await flushPromises()
  expect(view().get<HTMLImageElement>('img.preview').element.style.display).toBe('')
  expect(view().get('img.original').attributes('src')).toContain('/media/original/5')
  expect(view().get<HTMLImageElement>('img.original').element.style.display).toBe('none')
  await view().get('img.original').trigger('load')
  expect(view().get<HTMLImageElement>('img.preview').element.style.display).toBe('none')
  expect(view().get<HTMLImageElement>('img.original').element.style.display).toBe('')
})

it('keeps a preview without requesting an original that the asset cannot display', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, id: 5, video: null })
  wrapper = mountViewer(fakeSource())
  await flushPromises()
  expect(view().find('img.original').exists()).toBe(false)
  expect(view().get('img.preview').attributes('src')).toContain('/media/thumb/5/')
})

it('falls back to conversion at the source time and restarts a converted seek without the playback clock moving its thumb', async () => {
  wrapper = mountViewer()
  await flushPromises()
  let video = view().get<HTMLVideoElement>('video')
  video.element.currentTime = 12
  await video.trigger('timeupdate')
  await video.trigger('error')
  await flushPromises()
  video = view().get<HTMLVideoElement>('video')
  expect(video.attributes('src')).toContain('at=12000')
  const slider = wrapper.findComponent(USlider)
  slider.vm.$emit('update:modelValue', 25000)
  await flushPromises()
  video.element.currentTime = 3
  await video.trigger('timeupdate')
  expect(view().get('[role="slider"]').attributes('aria-valuenow')).toBe('25000')
  slider.vm.$emit('change', new Event('change'))
  await flushPromises()
  expect(view().get('video').attributes('src')).toContain('at=25000')
  await view().get('video').trigger('error')
  expect(view().get('[role="alert"]').text()).toContain('could not be played')
})

it('seeks direct playback without adding the initial search offset', async () => {
  wrapper = mountViewer()
  await flushPromises()
  const slider = wrapper.findComponent(USlider)
  slider.vm.$emit('update:modelValue', 25000)
  slider.vm.$emit('change', new Event('change'))
  await flushPromises()
  expect(view().get<HTMLVideoElement>('video').element.currentTime).toBe(25)
})

it('shares favourite actions with the command registry and preserves upper-case shortcuts', async () => {
  const favourite = vi.fn()
  wrapper = mountViewer(fakeSource({ favourite }))
  await flushPromises()
  expect(commandById('viewer.favourite')?.zone).toBe('viewer')
  await runCommand('viewer.favourite')
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'F', shiftKey: true }))
  expect(favourite).toHaveBeenCalledTimes(2)
})

it('leaves Escape and arrows to a confirmation above the viewer', async () => {
  const source = fakeSource()
  const Host = defineComponent({ render: () => h('div', [h(Viewer, { source }), h(ConfirmDialog)]) })
  wrapper = mountInApp(Host, { plugins: [getTestPinia()] })
  const answer = confirm('Confirm', 'Confirm action', 'Continue')
  await flushPromises()
  expect(document.body.querySelector('[data-confirm="cancel"]')).not.toBeNull()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }))
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
  expect(source.step).not.toHaveBeenCalled()
  expect(source.close).not.toHaveBeenCalled()
  await view().get('[data-confirm="cancel"]').trigger('click')
  expect(await answer).toBe(false)
  await flushPromises()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }))
  expect(source.step).toHaveBeenCalledOnce()
})

it('keeps keyboard focus in the viewer and returns it when the viewer unmounts', async () => {
  const trigger = document.createElement('button')
  document.body.appendChild(trigger)
  trigger.focus()
  wrapper = mountViewer(fakeSource())
  await flushPromises()
  const next = view().get<HTMLButtonElement>('.next').element
  next.focus()
  next.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
  expect(document.activeElement).toBe(view().get('button[aria-label="Close"]').element)
  wrapper.unmount()
  wrapper = undefined
  expect(document.activeElement).toBe(trigger)
})

it('shows a failed detail read without leaving a preview of an inaccessible asset visible', async () => {
  vi.mocked(engine.asset).mockRejectedValueOnce(new Error('not found'))
  wrapper = mountViewer()
  await flushPromises()
  expect(view().get('[role="alert"]').text()).toContain('not found')
  expect(view().find('.media img').exists()).toBe(false)
})

it('keeps Shift with the existing arrow and Escape shortcuts', async () => {
  const source = fakeSource()
  wrapper = mountViewer(source)
  await flushPromises()
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', shiftKey: true }))
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', shiftKey: true }))
  expect(source.step).toHaveBeenCalledExactlyOnceWith(1)
  expect(source.close).toHaveBeenCalledOnce()
})

it('uses converted playback when the WebView cannot decode HEVC', async () => {
  vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('')
  vi.mocked(engine.asset).mockResolvedValue({
    ...detail,
    video: { duration: 60000, codec: 'hevc', audio: 'aac', play: Playback.PlayHEVC },
  })
  wrapper = mountViewer()
  await flushPromises()
  expect(view().get('video').attributes('src')).toContain('at=10000')
})
