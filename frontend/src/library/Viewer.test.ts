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
import { t } from '../shared/i18n'
import { DateSource, engine, Kind, Playback, TrackKind, videosAvailable } from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail } from '../bridge'
import { useLibrary } from './store'
import type { ViewerSource } from './store'
import Viewer from './Viewer.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    asset: vi.fn(),
    albums: vi.fn().mockResolvedValue([]),
    inspect: vi.fn().mockResolvedValue({ tracks: [] }),
    render: vi.fn().mockRejectedValue(new Error('Not available')),
  },
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
const displayRestores: (() => void)[] = []
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
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
  vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
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
  for (const restore of displayRestores.splice(0).reverse()) restore()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})

it('shows the video without a still image covering playback', async () => {
  wrapper = mountViewer()
  await flushPromises()
  expect(view().find('video').exists()).toBe(true)
  expect(view().find('.media img').exists()).toBe(false)
  expect(view().get('video').attributes('controls')).toBeUndefined()
  expect(view().findAll('[role="slider"][aria-label="Seek video"]')).toHaveLength(1)
  expect(view().find('button[aria-label="Play"]').exists()).toBe(true)
})

it('says so when videos cannot play at all', async () => {
  vi.mocked(videosAvailable).mockReturnValue(false)
  wrapper = mountViewer()
  await flushPromises()
  expect(view().find('video').exists()).toBe(false)
  expect(view().find('.video-controls').exists()).toBe(false)
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
    caps: { ...detail.caps, directPlay: false },
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
  let fail: ((error: unknown) => void) | undefined
  const promise = new Promise<T>((resolve, reject) => {
    complete = resolve
    fail = reject
  })
  return {
    promise,
    resolve(value: T) {
      if (!complete) throw new Error('Deferred promise was not initialised')
      complete(value)
    },
    reject(error: unknown) {
      if (!fail) throw new Error('Deferred promise was not initialised')
      fail(error)
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

function simulatePlayback(el: HTMLVideoElement) {
  let paused = true
  Object.defineProperty(el, 'paused', { configurable: true, get: () => paused })
  const play = vi.spyOn(el, 'play').mockImplementation(() => {
    paused = false
    el.dispatchEvent(new Event('play'))
    return Promise.resolve()
  })
  const pause = vi.spyOn(el, 'pause').mockImplementation(() => {
    paused = true
    el.dispatchEvent(new Event('pause'))
  })
  return { play, pause }
}

it('plays and pauses with the custom controls and synchronizes native playback events', async () => {
  wrapper = mountViewer()
  await flushPromises()
  const video = view().get<HTMLVideoElement>('video')
  const playback = simulatePlayback(video.element)
  await view().get('button[aria-label="Play"]').trigger('click')
  expect(playback.play).toHaveBeenCalledOnce()
  await view().get('button[aria-label="Pause"]').trigger('click')
  expect(playback.pause).toHaveBeenCalledOnce()
  expect(view().find('button[aria-label="Play"]').exists()).toBe(true)
  await video.element.play()
  await flushPromises()
  expect(view().find('button[aria-label="Pause"]').exists()).toBe(true)
})

it('preserves pause, volume, mute and speed when a converted seek replaces the stream', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  let video = view().get<HTMLVideoElement>('video')
  simulatePlayback(video.element)
  await video.trigger('loadedmetadata')
  await view().get('button[aria-label="Play"]').trigger('click')
  await view().get('button[aria-label="Pause"]').trigger('click')
  const [seek, volume] = wrapper.findAllComponents(USlider)
  if (!seek || !volume) throw new Error('Missing playback sliders')
  volume.vm.$emit('update:modelValue', 35)
  await flushPromises()
  await view().get('button[aria-label="Mute"]').trigger('click')
  await view().get('select[aria-label="Playback speed"]').setValue('1.5')
  const oldElement = video.element
  seek.vm.$emit('update:modelValue', 25000)
  seek.vm.$emit('change', new Event('change'))
  await flushPromises()
  video = view().get<HTMLVideoElement>('video')
  await video.trigger('loadedmetadata')
  expect(video.element).not.toBe(oldElement)
  expect(video.attributes('src')).toContain('at=25000')
  expect(video.element.autoplay).toBe(false)
  expect(video.element.volume).toBe(0.35)
  expect(video.element.muted).toBe(true)
  expect(video.element.playbackRate).toBe(1.5)
  await view().get('button[aria-label="Unmute"]').trigger('click')
  expect(video.element.muted).toBe(false)
  expect(video.element.volume).toBe(0.35)
})

it('preserves playing intent when a discarded converted stream emits pause or error', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  const old = view().get<HTMLVideoElement>('video')
  simulatePlayback(old.element)
  await old.trigger('loadedmetadata')
  await view().get('button[aria-label="Play"]').trigger('click')
  const slider = wrapper.findComponent(USlider)
  slider.vm.$emit('update:modelValue', 25000)
  slider.vm.$emit('change', new Event('change'))
  await flushPromises()
  old.element.dispatchEvent(new Event('error'))
  await flushPromises()
  expect(view().get<HTMLVideoElement>('video').element).not.toBe(old.element)
  expect(view().get<HTMLVideoElement>('video').element.autoplay).toBe(true)
  expect(view().find('[role="alert"]').exists()).toBe(false)
})

it('ignores buffering and picture-in-picture events from a discarded converted stream', async () => {
  displayProperty(document, 'pictureInPictureEnabled', true)
  displayProperty(HTMLVideoElement.prototype, 'requestPictureInPicture', vi.fn())
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  const old = view().get<HTMLVideoElement>('video')
  await old.trigger('loadedmetadata')
  const slider = wrapper.findComponent(USlider)
  slider.vm.$emit('update:modelValue', 25000)
  slider.vm.$emit('change', new Event('change'))
  await flushPromises()
  old.element.dispatchEvent(new Event('canplay'))
  await flushPromises()
  expect(view().text()).toContain(t('viewer.buffering'))
  const current = view().get<HTMLVideoElement>('video')
  await current.trigger('canplay')
  old.element.dispatchEvent(new Event('waiting'))
  old.element.dispatchEvent(new Event('enterpictureinpicture'))
  await flushPromises()
  expect(view().text()).not.toContain(t('viewer.buffering'))
  expect(view().find('button[aria-label="Exit picture in picture"]').exists()).toBe(false)
  expect(view().get('button[aria-label="Picture in picture"]').attributes('aria-pressed')).toBe('false')
})

it('scrubs with the indexed sprite instead of generating arbitrary cached frames', async () => {
  wrapper = mountViewer()
  await flushPromises()
  wrapper.findComponent(USlider).vm.$emit('update:modelValue', 25000)
  await flushPromises()
  const preview = view().get('.seek-preview')
  expect(preview.element.tagName).toBe('SPAN')
  expect(preview.attributes('style')).toContain('/media/sprite/1?v=1')
  expect(view().find('img[src*="/media/frame/"]').exists()).toBe(false)
})

it('removes existing subtitle cues even when the reused text track was disabled', async () => {
  vi.mocked(engine.inspect).mockResolvedValueOnce({
    orientation: 1,
    hasProfile: false,
    profile: '',
    lens: '',
    exposure: '',
    aperture: '',
    iso: '',
    frameRate: '',
    pages: 0,
    tracks: [{ index: 3, kind: TrackKind.TrackSubtitle, codec: 'subrip', language: 'eng', title: '', supported: true }],
  })
  vi.spyOn(globalThis, 'fetch').mockImplementation(() =>
    Promise.resolve(new Response('WEBVTT\n\n00:00:00.000 --> 00:00:03.000\nOne caption\n', { status: 200 })),
  )
  class Cue {
    constructor(
      readonly startTime: number,
      readonly endTime: number,
      readonly text: string,
    ) {}
  }
  vi.stubGlobal('VTTCue', Cue)
  const cues: Cue[] = []
  const track = {
    mode: 'disabled',
    get cues() {
      return this.mode === 'disabled' ? null : cues
    },
    addCue: (cue: Cue) => cues.push(cue),
    removeCue: (cue: Cue) => cues.splice(cues.indexOf(cue), 1),
  }
  wrapper = mountViewer()
  await flushPromises()
  const addTrack = vi.fn(() => track)
  Object.defineProperty(view().get('video').element, 'addTextTrack', { value: addTrack })
  for (let i = 0; i < 3; i++) {
    await view().get('select[aria-label="Subtitle track"]').setValue('3')
    await flushPromises()
    expect(cues).toHaveLength(1)
    await view().get('select[aria-label="Subtitle track"]').setValue('-1')
    await flushPromises()
    expect(cues).toHaveLength(0)
  }
  expect(addTrack).toHaveBeenCalledOnce()
})

it('loops converted playback through a new source without replaying the discarded offset', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  const old = view().get<HTMLVideoElement>('video')
  const playback = simulatePlayback(old.element)
  await view().get('button[aria-label="Loop"]').trigger('click')
  await old.trigger('ended')
  await flushPromises()
  expect(playback.play).not.toHaveBeenCalled()
  expect(view().get('video').attributes('src')).toContain('at=0')
  expect(view().get<HTMLVideoElement>('video').element.autoplay).toBe(true)
})

it('replays a converted video from the beginning instead of the last stream offset', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  await view().get('video').trigger('ended')
  expect(view().get('[role="slider"][aria-label="Seek video"]').attributes('aria-valuenow')).toBe('60000')
  await view().get('button[aria-label="Replay"]').trigger('click')
  await flushPromises()
  expect(view().get('video').attributes('src')).toContain('at=0')
  expect(view().get<HTMLVideoElement>('video').element.autoplay).toBe(true)
})

it('clamps skips to the source bounds and avoids an empty converted stream at the end', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  await view().get('video').trigger('loadedmetadata')
  await view().get('button[aria-label="Back 10 seconds"]').trigger('click')
  expect(view().get('video').attributes('src')).toContain('at=0')
  await view().get('video').trigger('loadedmetadata')
  await view().get('button[aria-label="Back 10 seconds"]').trigger('click')
  expect(view().get('video').attributes('src')).toContain('at=0')
  const slider = wrapper.findComponent(USlider)
  slider.vm.$emit('update:modelValue', 60000)
  slider.vm.$emit('change', new Event('change'))
  await flushPromises()
  expect(view().get('video').attributes('src')).toContain('at=59999')
})

it('reports rejected play requests and leaves the Play button available to retry', async () => {
  wrapper = mountViewer()
  await flushPromises()
  const video = view().get<HTMLVideoElement>('video')
  const play = vi.spyOn(video.element, 'play').mockRejectedValue(new Error('autoplay blocked'))
  await view().get('button[aria-label="Play"]').trigger('click')
  await flushPromises()
  expect(play).toHaveBeenCalledOnce()
  expect(view().get('.notice').text()).toContain('Press Play to try again')
  expect(view().find('button[aria-label="Play"]').exists()).toBe(true)
})

it('keeps a rejected play from a replaced stream from changing the current controls', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  const pending = deferred<undefined>()
  vi.spyOn(view().get<HTMLVideoElement>('video').element, 'play').mockReturnValue(pending.promise)
  await view().get('button[aria-label="Play"]').trigger('click')
  const slider = wrapper.findComponent(USlider)
  slider.vm.$emit('update:modelValue', 25000)
  slider.vm.$emit('change', new Event('change'))
  await flushPromises()
  pending.reject(new Error('old stream removed'))
  await flushPromises()
  expect(view().find('.notice').exists()).toBe(false)
  expect(view().get<HTMLVideoElement>('video').element.autoplay).toBe(true)
})

it('keeps the speed selector arrows in the player and supports playback keys on the video', async () => {
  wrapper = mountViewer()
  await flushPromises()
  const neighbour = vi.spyOn(useLibrary(), 'neighbour').mockResolvedValue(null)
  await view().get('select[aria-label="Playback speed"]').trigger('keydown', { key: 'ArrowRight' })
  const video = view().get<HTMLVideoElement>('video')
  await video.trigger('loadedmetadata')
  const playback = simulatePlayback(video.element)
  await video.trigger('keydown', { key: ' ' })
  expect(playback.play).toHaveBeenCalledOnce()
  await video.trigger('keydown', { key: 'ArrowRight' })
  expect(video.element.currentTime).toBe(20)
  expect(neighbour).not.toHaveBeenCalled()
})

it('restores audible volume when unmuting a zero-volume video', async () => {
  wrapper = mountViewer()
  await flushPromises()
  const video = view().get<HTMLVideoElement>('video')
  video.element.volume = 0
  video.element.muted = false
  await video.trigger('volumechange')
  await view().get('button[aria-label="Unmute"]').trigger('click')
  expect(video.element.volume).toBe(1)
  expect(video.element.muted).toBe(false)
})

it('commits the new seek value when the slider sends its keyboard commit before its model update', async () => {
  wrapper = mountViewer()
  await flushPromises()
  const video = view().get<HTMLVideoElement>('video')
  await video.trigger('loadedmetadata')
  const slider = view().get('[role="slider"][aria-label="Seek video"]')
  await slider.trigger('keydown', { key: 'ArrowRight' })
  await flushPromises()
  expect(video.element.currentTime).toBe(11)
  await slider.trigger('keydown', { key: 'Home' })
  await flushPromises()
  expect(video.element.currentTime).toBe(0)
  video.element.currentTime = 3
  await video.trigger('timeupdate')
  expect(slider.attributes('aria-valuenow')).toBe('3000')
})

it('releases the seek thumb when a cancelled drag sends no change event', async () => {
  wrapper = mountViewer()
  await flushPromises()
  const video = view().get<HTMLVideoElement>('video')
  await video.trigger('loadedmetadata')
  wrapper.findComponent(USlider).vm.$emit('update:modelValue', 15000)
  await flushPromises()
  await view().get('.seek-control').trigger('pointercancel')
  await flushPromises()
  expect(video.element.currentTime).toBe(15)
  video.element.currentTime = 16
  await video.trigger('timeupdate')
  expect(view().get('[role="slider"][aria-label="Seek video"]').attributes('aria-valuenow')).toBe('16000')
})

it('rewinds the existing converted stream when seeking back to its own starting time', async () => {
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  const video = view().get<HTMLVideoElement>('video')
  video.element.currentTime = 5
  await video.trigger('timeupdate')
  const slider = wrapper.findComponent(USlider)
  slider.vm.$emit('update:modelValue', 10000)
  slider.vm.$emit('change', new Event('change'))
  await flushPromises()
  expect(view().get('video').element).toBe(video.element)
  expect(video.element.currentTime).toBe(0)
})

function displayProperty(object: object, key: PropertyKey, value: unknown) {
  const previous = Object.getOwnPropertyDescriptor(object, key)
  Object.defineProperty(object, key, { configurable: true, value })
  displayRestores.push(() => {
    if (previous) Object.defineProperty(object, key, previous)
    else Reflect.deleteProperty(object, key)
  })
}

it('keeps fullscreen controls and the viewer open across converted stream replacements', async () => {
  const enter = vi.fn(function (this: HTMLElement) {
    displayProperty(document, 'fullscreenElement', this)
    document.dispatchEvent(new Event('fullscreenchange'))
    return Promise.resolve()
  })
  const exit = vi.fn(() => {
    displayProperty(document, 'fullscreenElement', null)
    document.dispatchEvent(new Event('fullscreenchange'))
    return Promise.resolve()
  })
  displayProperty(document, 'fullscreenEnabled', true)
  displayProperty(HTMLElement.prototype, 'requestFullscreen', enter)
  displayProperty(document, 'exitFullscreen', exit)
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  await view().get('button[aria-label="Fullscreen"]').trigger('click')
  const fullscreenPlayer = document.fullscreenElement
  expect(fullscreenPlayer).toBe(view().get('.video-player').element)
  expect(fullscreenPlayer?.querySelector('.video-controls')).not.toBeNull()
  const slider = wrapper.findComponent(USlider)
  slider.vm.$emit('update:modelValue', 25000)
  slider.vm.$emit('change', new Event('change'))
  await flushPromises()
  expect(document.fullscreenElement).toBe(fullscreenPlayer)
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
  expect(useLibrary().viewer).not.toBeNull()
  await view().get('button[aria-label="Exit fullscreen"]').trigger('click')
  expect(exit).toHaveBeenCalledOnce()
  expect(document.fullscreenElement).toBeNull()
})

it('synchronizes picture-in-picture controls with browser events and exits on close', async () => {
  const enter = vi.fn(function (this: HTMLVideoElement) {
    displayProperty(document, 'pictureInPictureElement', this)
    this.dispatchEvent(new Event('enterpictureinpicture'))
    return Promise.resolve()
  })
  const exit = vi.fn(() => {
    const el = document.pictureInPictureElement
    displayProperty(document, 'pictureInPictureElement', null)
    el?.dispatchEvent(new Event('leavepictureinpicture'))
    return Promise.resolve()
  })
  displayProperty(document, 'pictureInPictureEnabled', true)
  displayProperty(HTMLVideoElement.prototype, 'requestPictureInPicture', enter)
  displayProperty(document, 'exitPictureInPicture', exit)
  wrapper = mountViewer()
  await flushPromises()
  await view().get('video').trigger('loadedmetadata')
  await view().get('button[aria-label="Picture in picture"]').trigger('click')
  expect(enter).toHaveBeenCalledOnce()
  await view().get('button[aria-label="Exit picture in picture"]').trigger('click')
  expect(exit).toHaveBeenCalledOnce()
  await view().get('button[aria-label="Picture in picture"]').trigger('click')
  wrapper.unmount()
  wrapper = undefined
  expect(exit).toHaveBeenCalledTimes(2)
})

it('reports declined fullscreen requests without breaking playback controls', async () => {
  displayProperty(document, 'fullscreenEnabled', true)
  displayProperty(HTMLElement.prototype, 'requestFullscreen', vi.fn().mockRejectedValue(new Error('unavailable')))
  wrapper = mountViewer()
  await flushPromises()
  await view().get('button[aria-label="Fullscreen"]').trigger('click')
  await flushPromises()
  expect(view().get('.notice').text()).toContain('Fullscreen is unavailable')
  expect(view().find('button[aria-label="Play"]').exists()).toBe(true)
})

it('clears picture-in-picture state when a converted seek replaces its video element', async () => {
  displayProperty(document, 'pictureInPictureEnabled', true)
  displayProperty(HTMLVideoElement.prototype, 'requestPictureInPicture', vi.fn().mockResolvedValue(undefined))
  vi.mocked(engine.asset).mockResolvedValue({ ...detail, caps: { ...detail.caps, directPlay: false } })
  wrapper = mountViewer()
  await flushPromises()
  await view().get('video').trigger('enterpictureinpicture')
  expect(view().find('button[aria-label="Exit picture in picture"]').exists()).toBe(true)
  const slider = wrapper.findComponent(USlider)
  slider.vm.$emit('update:modelValue', 25000)
  slider.vm.$emit('change', new Event('change'))
  await flushPromises()
  expect(view().find('button[aria-label="Picture in picture"]').exists()).toBe(true)
})

it('continues playback after a decoder failure pauses the direct video before conversion', async () => {
  wrapper = mountViewer()
  await flushPromises()
  const video = view().get<HTMLVideoElement>('video')
  const playback = simulatePlayback(video.element)
  await video.element.play()
  Object.defineProperty(video.element, 'error', { configurable: true, value: { code: 3 } })
  playback.pause()
  await video.trigger('error')
  await flushPromises()
  expect(view().get('video').attributes('src')).toContain('/play/')
  expect(view().get<HTMLVideoElement>('video').element.autoplay).toBe(true)
})
