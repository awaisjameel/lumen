import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { DateSource, engine, Kind, Playback, TrackKind } from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail } from '../bridge'
import { mountInApp } from '../shared/testing'
import { usePreferences } from '../shared/preferences'
import VideoPlayer from './VideoPlayer.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    inspect: vi.fn().mockResolvedValue({ tracks: [] }),
    playbackPosition: vi.fn().mockResolvedValue(12000),
    setPlaybackPosition: vi.fn().mockResolvedValue(undefined),
    setPreferences: vi.fn().mockResolvedValue(undefined),
  },
}))
const detail: AssetDetail = {
  id: 1,
  kind: Kind.KindVideo,
  takenAt: 0,
  dateFrom: DateSource.DateFromFile,
  width: 1920,
  height: 1080,
  camera: '',
  lat: null,
  lon: null,
  placeId: 0,
  favourite: false,
  paths: ['D:/Photos/waterfront.mp4'],
  size: 100,
  ext: '.mp4',
  caps: { preview: true, original: false, render: false, paged: false, directPlay: true, convertPlay: false },
  video: { duration: 60000, codec: 'h264', audio: 'aac', play: Playback.PlayDirect },
}
let wrapper: VueWrapper | undefined
const body = () => new DOMWrapper(document.body)
function mountPlayer(privateView = false, remember = false) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const prefs = usePreferences()
  prefs.prefs = { ...prefs.prefs, viewer: { ...prefs.viewer, resume: remember } }
  return mountInApp(VideoPlayer, { props: { detail, at: 0, thumbVersion: 1, private: privateView }, plugins: [pinia] })
}
beforeEach(() => {
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
  vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})

it('previews the hovered source time without moving the playhead', async () => {
  wrapper = mountPlayer()
  await flushPromises()
  const video = body().get<HTMLVideoElement>('video')
  await video.trigger('loadedmetadata')
  const timeline = body().get('[data-video-timeline]')
  vi.spyOn(timeline.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(10, 0, 600, 32))
  timeline.element.dispatchEvent(new MouseEvent('pointermove', { clientX: 310, bubbles: true }))
  await flushPromises()
  expect(body().text()).toContain('0:30')
  expect(body().get('[aria-label="Seek video"]').attributes('aria-valuenow')).toBe('0')
  expect(video.element.currentTime).toBe(0)
  await timeline.trigger('pointerleave')
  expect(body().text()).not.toContain('0:30')
})

it('shows each buffered range and clears it when retrying a failed source', async () => {
  wrapper = mountPlayer()
  await flushPromises()
  const video = body().get<HTMLVideoElement>('video')
  Object.defineProperty(video.element, 'buffered', {
    value: { length: 2, start: (i: number) => (i === 0 ? 0 : 30), end: (i: number) => (i === 0 ? 10 : 45) },
  })
  await video.trigger('progress')
  expect(body().findAll('[data-buffered-range]')).toHaveLength(2)
  expect(body().findAll('[data-buffered-range]')[1]?.attributes('style')).toContain('left: 50%')
  await video.trigger('error')
  expect(body().get('[role="alert"]').text()).toContain('Reconnect the drive')
  const retry = body()
    .findAll('button')
    .find((button) => button.text() === 'Try again')
  if (!retry) throw new Error('Missing retry action')
  await retry.trigger('click')
  await flushPromises()
  expect(body().get('video').element).not.toBe(video.element)
  expect(body().findAll('[data-buffered-range]')).toHaveLength(0)
  expect(body().find('[role="alert"]').exists()).toBe(false)
})

it('keeps speed and track controls in the playback settings popover', async () => {
  wrapper = mountPlayer()
  await flushPromises()
  expect(body().find('select[aria-label="Playback speed"]').exists()).toBe(false)
  await body().get('button[aria-label="Playback settings"]').trigger('click')
  await flushPromises()
  expect(body().get('select[aria-label="Subtitle track"]').attributes('disabled')).toBeDefined()
  expect(body().text()).toContain('This video has no subtitle tracks.')
  await body().get('select[aria-label="Playback speed"]').setValue('1.5')
  expect(body().get<HTMLVideoElement>('video').element.playbackRate).toBe(1.5)
  await body().get('button[aria-label="Close playback settings"]').trigger('click')
  await flushPromises()
  expect(body().find('select[aria-label="Playback speed"]').exists()).toBe(false)
})

it('shows unavailable picture-in-picture and fullscreen controls with reasons', async () => {
  wrapper = mountPlayer()
  await flushPromises()
  expect(body().get('button[aria-label="Picture in picture"]').attributes('disabled')).toBeDefined()
  expect(body().get('button[aria-label="Fullscreen"]').attributes('disabled')).toBeDefined()
})

it('never reads or writes resume history in a private viewer', async () => {
  wrapper = mountPlayer(true, true)
  await flushPromises()
  await body().get('video').trigger('timeupdate')
  wrapper.unmount()
  wrapper = undefined
  expect(engine.playbackPosition).not.toHaveBeenCalled()
  expect(engine.setPlaybackPosition).not.toHaveBeenCalled()
})

it('clears a subtitle failure after retrying the same selected track', async () => {
  vi.mocked(engine.inspect).mockResolvedValueOnce({
    orientation: 1,
    hasProfile: false,
    profile: '',
    lens: '',
    exposure: '',
    aperture: '',
    iso: '',
    frameRate: '',
    pages: 1,
    tracks: [{ index: 3, kind: TrackKind.TrackSubtitle, codec: 'subrip', language: 'eng', title: '', supported: true }],
  })
  vi.spyOn(globalThis, 'fetch')
    .mockRejectedValueOnce(new Error('Unavailable'))
    .mockResolvedValue(new Response('WEBVTT'))
  wrapper = mountPlayer()
  await flushPromises()
  Object.defineProperty(body().get('video').element, 'addTextTrack', {
    value: () => ({ mode: 'disabled', cues: [], addCue: vi.fn(), removeCue: vi.fn() }),
  })
  await body().get('button[aria-label="Subtitle track"]').trigger('click')
  await flushPromises()
  await body().get('select[aria-label="Subtitle track"]').setValue('3')
  await flushPromises()
  expect(body().text()).toContain('Subtitles could not be loaded.')
  const retry = body()
    .findAll('button')
    .find((button) => button.text() === 'Try again')
  if (!retry) throw new Error('Missing subtitle retry')
  await retry.trigger('click')
  await flushPromises()
  expect(globalThis.fetch).toHaveBeenCalledTimes(2)
  expect(body().text()).not.toContain('Subtitles could not be loaded.')
})
