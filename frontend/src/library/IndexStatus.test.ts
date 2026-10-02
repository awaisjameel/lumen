import { beforeEach, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { Device } from '../bridge'
import type { Progress } from '../bridge'
import { useLibrary } from './store'
import IndexStatus from './IndexStatus.vue'

const idle: Progress = {
  scanning: false,
  total: 100,
  metadata: 0,
  thumbnail: 0,
  embedding: 0,
  faces: 0,
  ocr: 0,
  errors: 0,
  skipped: 0,
  paused: false,
  onBattery: false,
  ai: { device: Device.DeviceCPU, error: '', facesError: '' },
}

function show(progress: Partial<Progress>) {
  useLibrary().progress = { ...idle, ...progress }
  return mount(IndexStatus)
}

beforeEach(() => setActivePinia(createPinia()))

it('counts the photos still waiting for their text', () => {
  const status = show({ ocr: 12 })
  expect(status.text()).toContain('Reading text in your photos… 12 left')
  expect(status.find('.bar').exists()).toBe(true)
})

it('says the library is up to date once the text is read', () => {
  expect(show({ ocr: 0 }).text()).toContain('Up to date')
})

it('puts the photos first and the text after, and waits for mains power', () => {
  expect(show({ embedding: 5, ocr: 12 }).text()).toContain('Understanding your photos… 5 left')
  expect(show({ ocr: 12, onBattery: true }).text()).toContain('Waiting for mains power · 12 left')
})

it('shows a pause with the text left to read', () => {
  expect(show({ ocr: 3, paused: true }).text()).toContain('Paused · 3 left')
})
