import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import SettingsView from './SettingsView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    setTagConfidence: vi.fn<() => Promise<void>>().mockResolvedValue(),
    setVideos: vi.fn<() => Promise<void>>(),
    setEventGapHours: vi.fn<(hours: number) => Promise<void>>(),
  },
}))

beforeEach(() => vi.clearAllMocks())

it('persists the final slider value when leaving settings before the debounce', async () => {
  setActivePinia(createPinia())
  vi.spyOn(useLibrary(), 'refresh').mockResolvedValue()
  const wrapper = mount(SettingsView)
  await wrapper.get('#confidence').setValue('60')
  const value = useLibrary().settings.tagConfidence
  expect(engine.setTagConfidence).not.toHaveBeenCalled()
  wrapper.unmount()
  await flushPromises()
  expect(engine.setTagConfidence).toHaveBeenCalledExactlyOnceWith(value)
})

it('puts the videos checkbox back when the engine refuses', async () => {
  setActivePinia(createPinia())
  vi.mocked(engine.setVideos).mockRejectedValueOnce(new Error('database is locked'))
  const wrapper = mount(SettingsView)
  const box = wrapper.get<HTMLInputElement>('.toggle input')
  await box.setValue(true)
  await flushPromises()
  expect(useLibrary().settings.videos).toBe(false)
  expect(box.element.checked).toBe(false)
  expect(wrapper.text()).toContain('database is locked')
  wrapper.unmount()
})

it('serializes gap writes and ignores a superseded failure', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  vi.spyOn(lib, 'refresh').mockResolvedValue()
  let fail!: (reason: Error) => void
  vi.mocked(engine.setEventGapHours)
    .mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
    .mockResolvedValueOnce()
  const wrapper = mount(SettingsView)
  const input = wrapper.get<HTMLInputElement>('.event-gap input')
  await input.setValue('24')
  await input.setValue('36')
  await input.setValue('48')
  expect(engine.setEventGapHours).toHaveBeenCalledExactlyOnceWith(24)
  fail(new Error('older save failed'))
  await flushPromises()
  expect(engine.setEventGapHours).toHaveBeenNthCalledWith(2, 48)
  expect(lib.settings.eventGapHours).toBe(48)
  expect(input.element.value).toBe('48')
  expect(wrapper.text()).not.toContain('older save failed')
  wrapper.unmount()
})

it('rolls a failed latest gap write back to the last persisted value', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  vi.spyOn(lib, 'refresh').mockResolvedValue()
  let finish!: () => void
  vi.mocked(engine.setEventGapHours)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockRejectedValueOnce(new Error('latest save failed'))
  const wrapper = mount(SettingsView)
  const input = wrapper.get<HTMLInputElement>('.event-gap input')
  await input.setValue('24')
  await input.setValue('36')
  finish()
  await flushPromises()
  expect(lib.settings.eventGapHours).toBe(24)
  expect(input.element.value).toBe('24')
  expect(wrapper.text()).toContain('latest save failed')
  wrapper.unmount()
})

it('persists the queued gap value after leaving settings', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  vi.spyOn(lib, 'refresh').mockResolvedValue()
  let finish!: () => void
  vi.mocked(engine.setEventGapHours)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockResolvedValueOnce()
  const wrapper = mount(SettingsView)
  await wrapper.get('.event-gap input').setValue('24')
  await wrapper.get('.event-gap input').setValue('36')
  wrapper.unmount()
  finish()
  await flushPromises()
  expect(engine.setEventGapHours).toHaveBeenNthCalledWith(2, 36)
  expect(lib.settings.eventGapHours).toBe(36)
})

it.each([
  ['', 12],
  ['0', 1],
  ['100', 72],
  ['12.4', 12],
] as const)('normalizes gap input %s to %s', async (entered, expected) => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  vi.spyOn(lib, 'refresh').mockResolvedValue()
  vi.mocked(engine.setEventGapHours).mockResolvedValue()
  const wrapper = mount(SettingsView)
  const input = wrapper.get<HTMLInputElement>('.event-gap input')
  await input.setValue(entered)
  await flushPromises()
  expect(lib.settings.eventGapHours).toBe(expected)
  expect(input.element.value).toBe(String(expected))
  expect(engine.setEventGapHours).toHaveBeenCalledExactlyOnceWith(expected)
  wrapper.unmount()
})
