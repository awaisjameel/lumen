import { beforeEach, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { Device } from '../bridge'
import type { Progress } from '../bridge'
import { useLibrary } from './store'
import IndexStatus from './IndexStatus.vue'

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
  ai: { device: Device.DeviceCPU, error: '', facesError: '' },
}

function show(progress: Partial<Progress>, working: Partial<Progress['working']> = {}) {
  useLibrary().progress = { ...idle, ...progress, working: { ...idle.working, ...working } }
  return mount(IndexStatus)
}

const label = (status: ReturnType<typeof show>) => status.find('.label').text()
const rows = (status: ReturnType<typeof show>) => status.findAll('.queue li').map((li) => li.text())

beforeEach(() => setActivePinia(createPinia()))

it('says what is at work, not only that a scan runs', () => {
  const status = show(
    { scanning: true, scanPath: 'D:\\Downloads', scanSeen: 3500, thumbnail: 1234, faces: 800 },
    { thumbnail: true, faces: true },
  )
  expect(label(status)).toBe('Looking in Downloads')
  expect(status.text()).toContain('3,500 found')
  expect(status.find('.bar').classes()).toContain('indeterminate')
  expect(rows(status)).toEqual([
    expect.stringContaining('D:\\Downloads'),
    expect.stringMatching(/Making thumbnails\s*1,234 left/),
    expect.stringMatching(/Finding people\s*800 left/),
  ])
})

it('lists what runs now, then what waits its turn', () => {
  const status = show({ thumbnail: 40, embedding: 300, videos: 2, ocr: 12 }, { thumbnail: true })
  expect(label(status)).toBe('Making thumbnails')
  expect(status.text()).toContain('40 left')
  expect(status.findAll('.queue .head').map((h) => h.text())).toEqual(['Now', 'Next'])
  expect(status.findAll('.queue li.now').map((li) => li.text())).toEqual([expect.stringContaining('Making thumbnails')])
  expect(status.findAll('.queue li:not(.now)').map((li) => li.text())).toEqual([
    expect.stringContaining('Understanding your photos'),
    expect.stringContaining('Understanding your videos'),
    expect.stringContaining('Reading text in your photos'),
  ])
  // Screen readers reach the list from the pause button.
  expect(status.find('button').attributes('aria-describedby')).toBe(status.find('.queue').attributes('id'))
})

it('names the work that waits when nothing runs', () => {
  expect(label(show({ ocr: 12 }))).toBe('Reading text in your photos')
})

it('waits for mains power, and says so in the list', () => {
  const status = show({ embedding: 5, ocr: 12, onBattery: true })
  expect(label(status)).toBe('Waiting for mains power')
  expect(status.text()).toContain('5 left')
  expect(status.find('.queue .note').text()).toBe('The AI waits for mains power.')
  // What isn't the AI goes on, and leads.
  expect(label(show({ thumbnail: 3, embedding: 5, onBattery: true }, { thumbnail: true }))).toBe('Making thumbnails')
})

it('shows a pause with the work left', () => {
  const status = show({ ocr: 3, paused: true })
  expect(label(status)).toBe('Paused')
  expect(status.text()).toContain('3 left')
  expect(status.find('button').attributes('aria-label')).toBe('Resume')
  expect(status.find('.queue .note').text()).toBe('Everything waits until you resume.')
})

it('says the library is up to date, with nothing behind it', () => {
  const status = show({})
  expect(label(status)).toBe('Up to date')
  expect(status.text()).toContain('100 items')
  expect(status.find('.queue').exists()).toBe(false)
  expect(status.find('.bar').exists()).toBe(false)
})
