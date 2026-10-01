import { afterEach, beforeEach, expect, it, vi } from 'vitest'
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
    setAIOnBattery: vi.fn<() => Promise<void>>(),
    setEventGapHours: vi.fn<(hours: number) => Promise<void>>(),
  },
}))

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(engine.setTagConfidence).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setEventGapHours).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setVideos).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setAIOnBattery).mockReset().mockResolvedValue(undefined)
})
afterEach(() => vi.useRealTimers())

it.each(['videos', 'aiOnBattery'] as const)(
  'serializes %s saves across remounts and ignores an older failure',
  async (key) => {
    setActivePinia(createPinia())
    const lib = useLibrary()
    const save = vi.mocked(key === 'videos' ? engine.setVideos : engine.setAIOnBattery)
    let fail: (error: Error) => void = () => {
      throw new Error('the pending save has not started')
    }
    save
      .mockReturnValueOnce(
        new Promise((_resolve, reject) => {
          fail = reject
        }),
      )
      .mockResolvedValueOnce()
    const selector = key === 'videos' ? '.toggle input' : 'input[type="checkbox"]'
    const first = mount(SettingsView)
    const firstBox = key === 'videos' ? first.get(selector) : first.findAll(selector).at(-1)
    if (!firstBox) throw new Error('missing settings checkbox')
    await firstBox.setValue(true)
    first.unmount()
    const next = mount(SettingsView)
    try {
      const nextBox = key === 'videos' ? next.get(selector) : next.findAll(selector).at(-1)
      if (!nextBox) throw new Error('missing settings checkbox')
      await nextBox.setValue(false)
      expect(save).toHaveBeenCalledExactlyOnceWith(true)
      fail(new Error('older toggle failed'))
      await flushPromises()
      expect(save).toHaveBeenNthCalledWith(2, false)
      expect(lib.settings[key]).toBe(false)
      expect(next.text()).not.toContain('older toggle failed')
    } finally {
      next.unmount()
    }
  },
)

it.each(['videos', 'aiOnBattery'] as const)(
  'rolls back the latest failed %s save to the last persisted value',
  async (key) => {
    setActivePinia(createPinia())
    const lib = useLibrary()
    const save = vi.mocked(key === 'videos' ? engine.setVideos : engine.setAIOnBattery)
    save.mockResolvedValueOnce().mockRejectedValueOnce(new Error('latest toggle failed'))
    const set = key === 'videos' ? lib.saveVideos : lib.saveAIOnBattery
    await set(true)
    await set(false)
    expect(lib.settings[key]).toBe(true)
    expect(lib.settingsError).toContain('latest toggle failed')
  },
)

it('keeps a newer slider input when the previous save fails before its debounce fires', async () => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  const lib = useLibrary()
  let fail!: (reason: Error) => void
  vi.mocked(engine.setTagConfidence)
    .mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
    .mockResolvedValueOnce()
  const wrapper = mount(SettingsView)
  try {
    await wrapper.get('#confidence').setValue('70')
    await vi.advanceTimersByTimeAsync(250)
    await wrapper.get('#confidence').setValue('90')
    const newest = lib.settings.tagConfidence
    fail(new Error('older confidence failed'))
    await flushPromises()
    expect(lib.settings.tagConfidence).toBe(newest)
    expect(wrapper.get<HTMLInputElement>('#confidence').element.value).toBe('90')
    expect(wrapper.text()).not.toContain('older confidence failed')
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(engine.setTagConfidence).toHaveBeenLastCalledWith(newest)
  } finally {
    wrapper.unmount()
  }
})

it('uses one gap save queue across settings remounts', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  let finish!: () => void
  let persisted = 12
  const writes: number[] = []
  vi.mocked(engine.setEventGapHours).mockImplementation((hours) => {
    writes.push(hours)
    if (writes.length === 1)
      return new Promise((resolve) => {
        finish = () => {
          persisted = hours
          resolve()
        }
      })
    persisted = hours
    return Promise.resolve()
  })
  const first = mount(SettingsView)
  await first.get('.event-gap input').setValue('24')
  await first.get('.event-gap input').setValue('36')
  first.unmount()
  const next = mount(SettingsView)
  try {
    await next.get('.event-gap input').setValue('48')
    expect(writes).toEqual([24])
    finish()
    await flushPromises()
    expect(writes).toEqual([24, 48])
    expect(persisted).toBe(48)
    expect(lib.settings.eventGapHours).toBe(48)
    expect(next.get<HTMLInputElement>('.event-gap input').element.value).toBe('48')
  } finally {
    next.unmount()
  }
})

it('uses one confidence save queue across settings remounts', async () => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  const lib = useLibrary()
  let finish!: () => void
  let persisted = lib.settings.tagConfidence
  const writes: number[] = []
  vi.mocked(engine.setTagConfidence).mockImplementation((value) => {
    writes.push(value)
    if (writes.length === 1)
      return new Promise((resolve) => {
        finish = () => {
          persisted = value
          resolve()
        }
      })
    persisted = value
    return Promise.resolve()
  })
  const first = mount(SettingsView)
  await first.get('#confidence').setValue('50')
  await vi.advanceTimersByTimeAsync(250)
  await first.get('#confidence').setValue('60')
  first.unmount()
  const next = mount(SettingsView)
  try {
    await next.get('#confidence').setValue('80')
    const newest = lib.settings.tagConfidence
    await vi.advanceTimersByTimeAsync(250)
    expect(writes).toHaveLength(1)
    finish()
    await flushPromises()
    expect(writes).toHaveLength(2)
    expect(writes[1]).toBe(newest)
    expect(persisted).toBe(newest)
    expect(next.get<HTMLInputElement>('#confidence').element.value).toBe('80')
  } finally {
    next.unmount()
  }
})

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
