import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { Device, WaitReason } from '../bridge'
import { useLibrary } from '../library/store'
import { mountInApp } from '../shared/testing'
import FirstScan from './FirstScan.vue'

enableAutoUnmount(afterEach)

it('shows real thumbnails as they arrive and allows browsing while background work remains', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  const ensure = vi.spyOn(lib, 'ensureDays').mockResolvedValue()
  lib.progress = {
    scanning: true,
    scanPath: 'D:/Photos',
    scanSeen: 40,
    total: 40,
    metadata: 10,
    thumbnail: 20,
    embedding: 30,
    faces: 20,
    videos: 0,
    ocr: 0,
    working: { metadata: true, thumbnail: true, embedding: false, faces: false, videos: false, ocr: false },
    errors: 0,
    skipped: 0,
    paused: false,
    onBattery: false,
    waiting: WaitReason.WaitNone,
    ai: { device: Device.DeviceCPU, error: '', facesError: '' },
  }
  const wrapper = mountInApp(FirstScan)
  expect(wrapper.get('[role="progressbar"]').attributes('aria-valuenow')).toBeUndefined()
  expect(wrapper.text()).toContain('Waiting for the first photos…')
  lib.buckets = [{ day: '2026-10-16', count: 1 }]
  lib.days.set('2026-10-16', [{ id: 17, w: 100, h: 100, t: 2 }])
  lib.days = new Map(lib.days)
  await flushPromises()
  expect(ensure).toHaveBeenCalledWith(['2026-10-16'])
  expect(wrapper.find('img').attributes('src')).toContain('17')
  await wrapper.get('[data-browse]').trigger('click')
  expect(wrapper.findComponent(FirstScan).emitted('complete')).toHaveLength(1)
  lib.progress = {
    ...lib.progress,
    scanning: false,
    metadata: 0,
    thumbnail: 0,
    embedding: 0,
    faces: 0,
    working: { metadata: false, thumbnail: false, embedding: false, faces: false, videos: false, ocr: false },
  }
  await flushPromises()
  expect(wrapper.text()).toContain('Up to date')
  expect(wrapper.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('100')
})
