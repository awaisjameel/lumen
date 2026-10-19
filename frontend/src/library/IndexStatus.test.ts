import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { Device, engine, FailReason, WaitReason } from '../bridge'
import type { Progress } from '../bridge'
import { useLibrary } from './store'
import IndexStatus from './IndexStatus.vue'
import { commandById, runCommand } from '../shared/commands'
import { mountInApp } from '../shared/testing'

let mounted: VueWrapper | undefined
const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/settings', component: { render: () => null } }],
})

const idle: Progress = {
  scanning: false,
  scanPath: '',
  scanSeen: 0,
  total: 100,
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

function show(progress: Partial<Progress>, working: Partial<Progress['working']> = {}) {
  useLibrary().progress = { ...idle, ...progress, working: { ...idle.working, ...working } }
  mounted?.unmount()
  mounted = mountInApp(IndexStatus, { plugins: [router] })
  return mounted.findComponent(IndexStatus)
}

const label = (status: ReturnType<typeof show>) => status.get('[role="status"]').text()
const details = () => new DOMWrapper(document.body).get('[data-activity-details]')
const rows = () =>
  details()
    .findAll('li')
    .map((li) => li.text())
async function open(status: ReturnType<typeof show>) {
  await status.get('[data-activity-trigger]').trigger('click')
  await flushPromises()
}

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  mounted?.unmount()
  mounted = undefined
  vi.restoreAllMocks()
})

it('says what is at work, not only that a scan runs', async () => {
  const status = show(
    { scanning: true, scanPath: 'D:\\Downloads', scanSeen: 3500, thumbnail: 1234, faces: 800 },
    { thumbnail: true, faces: true },
  )
  expect(label(status)).toBe('Looking in Downloads')
  expect(status.text()).toContain('3,500 found')
  expect(status.find('[data-activity-progress]').attributes('data-indeterminate')).toBe('true')
  await open(status)
  expect(rows()).toEqual([
    expect.stringContaining('D:\\Downloads'),
    expect.stringMatching(/Making thumbnails\s*1,234 left/),
    expect.stringMatching(/Finding people\s*800 left/),
  ])
})

it('lists what runs now, then what waits its turn', async () => {
  const status = show({ thumbnail: 40, embedding: 300, videos: 2, ocr: 12 }, { thumbnail: true })
  expect(label(status)).toBe('Making thumbnails')
  expect(status.text()).toContain('40 left')
  await open(status)
  expect(
    details()
      .findAll('.head')
      .map((h) => h.text()),
  ).toEqual(['Now', 'Next'])
  expect(
    details()
      .findAll('[data-active="true"]')
      .map((li) => li.text()),
  ).toEqual([expect.stringContaining('Making thumbnails')])
  expect(
    details()
      .findAll('[data-active="false"]')
      .map((li) => li.text()),
  ).toEqual([
    expect.stringContaining('Understanding your photos'),
    expect.stringContaining('Understanding your videos'),
    expect.stringContaining('Reading text in your photos'),
  ])
  // Screen readers reach the list from the pause button.
  expect(status.get('[data-activity-trigger]').attributes('aria-describedby')).toBe(details().attributes('id'))
})

it('names the work that waits when nothing runs', () => {
  expect(label(show({ ocr: 12 }))).toBe('Reading text in your photos')
})

it.each([
  [WaitReason.WaitFullScreen, 'Waiting while a full-screen app is open', 'when the game, video or presentation closes'],
  [WaitReason.WaitBusy, 'Waiting while your PC is busy', 'when your other programs quieten down'],
  [WaitReason.WaitIdle, 'Waiting until you’re away', 'while you’re away from your PC'],
])('gives way to the PC (%s), naming why and what waits', async (waiting, text, note) => {
  const status = show({ thumbnail: 40, embedding: 300, waiting })
  expect(label(status)).toBe(text)
  expect(status.text()).toContain('40 left') // the work held back
  await open(status)
  expect(details().get('[data-wait-note]').text()).toContain(note)
  // With nothing left to do, there is nothing to wait for.
  expect(label(show({ waiting }))).toBe('Up to date')
})

it('waits for mains power, and says so in the list', async () => {
  const status = show({ embedding: 5, ocr: 12, onBattery: true })
  expect(label(status)).toBe('Waiting for mains power')
  expect(status.text()).toContain('5 left')
  await open(status)
  expect(details().get('[data-wait-note]').text()).toBe('The AI waits for mains power.')
  // What isn't the AI goes on, and leads.
  expect(label(show({ thumbnail: 3, embedding: 5, onBattery: true }, { thumbnail: true }))).toBe('Making thumbnails')
})

it('shows a pause with the work left', async () => {
  const status = show({ ocr: 3, paused: true })
  expect(label(status)).toBe('Paused')
  expect(status.text()).toContain('3 left')
  expect(status.find('[aria-label="Resume indexing"]').exists()).toBe(true)
  await open(status)
  expect(details().get('[data-wait-note]').text()).toBe('Everything waits until you resume.')
})

it('says the library is up to date, with nothing behind it', () => {
  const status = show({})
  expect(label(status)).toBe('Up to date')
  expect(status.text()).toContain('100 items')
  expect(status.find('[data-activity-details]').exists()).toBe(false)
  expect(status.find('[data-activity-progress]').exists()).toBe(false)
})

it('says why files couldn’t be read, reading it only when the list opens', async () => {
  const failures = vi.spyOn(engine, 'failures').mockResolvedValue([
    {
      reason: FailReason.FailDamaged,
      count: 5,
      files: [
        { path: 'D:\\Downloads\\icon.d.mts', error: 'ffprobe: icon.d.mts: Invalid data found when processing input' },
      ],
    },
    {
      reason: FailReason.FailSlow,
      count: 1,
      files: [{ path: 'D:\\Films\\GOAT.mp4', error: 'context deadline exceeded' }],
    },
  ])
  const status = show({ errors: 6 })
  expect(failures).not.toHaveBeenCalled()
  await open(status)
  await flushPromises()
  const reasons = details().findAll('[data-failure-reason]')
  expect(reasons.map((r) => r.text())).toEqual([
    expect.stringMatching(/^Damaged, or not really a photo or video\s*icon\.d\.mts\s*4 more\s*5$/),
    expect.stringMatching(/^Took too long to read\s*GOAT\.mp4\s*1$/),
  ])
  // The file's own error is a hover away.
  expect(details().find('[data-failure-file]').attributes('title')).toBe(
    'ffprobe: icon.d.mts: Invalid data found when processing input',
  )
  // Read again only once the count changes.
  await open(status)
  await open(status)
  expect(failures).toHaveBeenCalledTimes(1)
  // Nothing at work: the count itself takes focus, and leads to the list.
  expect(status.get('[data-activity-trigger]').attributes('aria-describedby')).toBe(details().attributes('id'))
})

it('shows loading and a retry when failure details cannot be read', async () => {
  let fail: (error: Error) => void = () => undefined
  const failures = vi
    .spyOn(engine, 'failures')
    .mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
    .mockResolvedValue([])
  const status = show({ errors: 3 })
  await open(status)
  expect(details().get('[aria-busy="true"]').text()).toBe('Loading failure details…')
  fail(new Error('Unavailable'))
  await flushPromises()
  expect(details().get('[role="alert"]').text()).toContain('Lumen couldn’t read the failure details.')
  await details()
    .findAll('button')
    .find((b) => b.text() === 'Try again')
    ?.trigger('click')
  await flushPromises()
  expect(failures).toHaveBeenCalledTimes(2)
  expect(details().find('[role="alert"]').exists()).toBe(false)
})

it('uses the same accessible details and pause control in the compact rail', async () => {
  useLibrary().progress = { ...idle, thumbnail: 5 }
  const pause = vi.spyOn(useLibrary(), 'setPaused').mockResolvedValue()
  mounted = mountInApp(IndexStatus, { props: { compact: true }, plugins: [router] })
  const status = mounted.findComponent(IndexStatus)
  await status.get('[aria-label="Pause indexing"]').trigger('click')
  expect(pause).toHaveBeenCalledExactlyOnceWith(true)
  await open(status)
  expect(details().text()).toContain('Making thumbnails')
  await details().get('[aria-label="Close activity details"]').trigger('click')
  await flushPromises()
  expect(status.get('[data-activity-trigger]').attributes('aria-expanded')).toBe('false')
})

it('shows AI failure reasons and the unsupported-format explanation even with no queued work', async () => {
  const status = show({
    ai: { device: Device.DeviceCPU, error: 'Image model unavailable', facesError: 'Face model unavailable' },
    skipped: 46,
  })
  expect(label(status)).toBe('Search is off')
  await open(status)
  expect(details().text()).toContain('Image model unavailable')
  expect(details().text()).toContain('Face model unavailable')
  expect(details().text()).toContain('the original files stay in their folders')
})

it('opens its activity from anywhere, as a search waiting for photos asks', async () => {
  show({ embedding: 40 }, { embedding: true })
  expect(new DOMWrapper(document.body).find('[data-activity-details]').exists()).toBe(false)
  expect(await runCommand('app.activity')).toBe(true)
  await flushPromises()
  expect(details().text()).toContain('Understanding your photos')
  mounted?.unmount()
  mounted = undefined
  expect(commandById('app.activity')).toBeUndefined()
})

it('leaves the activity command to the library’s own card, not a preview of one', () => {
  mounted = mountInApp(IndexStatus, { plugins: [router], props: { progress: idle } })
  expect(commandById('app.activity')).toBeUndefined()
})
