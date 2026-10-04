import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import USlider from '@nuxt/ui/components/Slider.vue'
import { nextTick } from 'vue'
import { mountInApp, recordNotifications } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { usePreferences } from '../shared/preferences'
import {
  Accent,
  Density,
  Theme,
  TEXT_SCALE_MIN,
  TEXT_SCALE_MAX,
  TEXT_SCALE_DEFAULT,
  AWAY_MINUTES,
  engine,
  THUMB_CACHE_DEFAULT_MB,
  THUMB_CACHE_MAX_MB,
  THUMB_CACHE_MIN_MB,
} from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import { t } from '../shared/i18n'
import SettingsView from './SettingsView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    migratePreferences: vi.fn<typeof Bridge.engine.migratePreferences>(() =>
      Promise.resolve({ ...usePreferences().prefs }),
    ),
    setTagConfidence: vi.fn<() => Promise<void>>().mockResolvedValue(),
    setVideos: vi.fn<() => Promise<void>>(),
    setAIOnBattery: vi.fn<() => Promise<void>>(),
    setPauseFullScreen: vi.fn<(on: boolean) => Promise<void>>(),
    setPauseBusy: vi.fn<(on: boolean) => Promise<void>>(),
    setIdleOnly: vi.fn<(on: boolean) => Promise<void>>(),
    setEventGapHours: vi.fn<(hours: number) => Promise<void>>(),
    setThumbCacheMB: vi.fn<(mb: number) => Promise<void>>(),
    setCPUThreads: vi.fn<(n: number) => Promise<void>>(),
    exportXMPNow: vi.fn<() => Promise<Bridge.XMPExport>>(),
    setPreferences: vi.fn<(prefs: Bridge.Preferences) => Promise<Bridge.Preferences>>(),
    thumbCacheBytes: vi.fn<() => Promise<number>>(),
    about: vi.fn<() => Promise<Bridge.About>>().mockResolvedValue({
      version: '0.9.0',
      dataDir: 'C:/Lumen',
      releasesURL: 'https://github.com/awaisjameel/lumen/releases',
      lastRunCrashed: false,
    }),
  },
}))

beforeEach(() => {
  localStorage.clear()
  vi.mocked(engine.setPreferences)
    .mockReset()
    .mockImplementation((prefs) => Promise.resolve(prefs))
  vi.clearAllMocks()
  vi.mocked(engine.setTagConfidence).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setEventGapHours).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setVideos).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setAIOnBattery).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setPauseFullScreen).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setPauseBusy).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setIdleOnly).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setThumbCacheMB).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setCPUThreads).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.exportXMPNow).mockReset().mockResolvedValue({ written: 0, unchanged: 0, skipped: 0 })
  vi.mocked(engine.thumbCacheBytes).mockReset().mockResolvedValue(0)
})
afterEach(() => vi.useRealTimers())

function mountSettings() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: SettingsView }],
  })
  return mountInApp(SettingsView, { plugins: [router] })
}
async function chooseAppearance(wrapper: ReturnType<typeof mountInApp>, group: string, index: number) {
  const choice = wrapper.get(`[data-appearance="${group}"]`).findAll('[role="radio"]').at(index)
  if (!choice) throw new Error(`missing ${group} choice`)
  await choice.trigger('click')
}

async function sliderInput(wrapper: ReturnType<typeof mountInApp>, id: string, value: number) {
  const slider = wrapper.findAllComponents(USlider).find((component) => component.attributes('id') === id)
  if (!slider) throw new Error(`missing ${id} slider`)
  slider.vm.$emit('update:modelValue', value)
  await nextTick()
}
async function sliderCommit(wrapper: ReturnType<typeof mountInApp>, id: string) {
  const slider = wrapper.findAllComponents(USlider).find((component) => component.attributes('id') === id)
  if (!slider) throw new Error(`missing ${id} slider`)
  slider.vm.$emit('change', new Event('change'))
  await nextTick()
}

// A toggle, found by its label: the sections hold several.
function toggle(wrapper: ReturnType<typeof mountInApp>, text: string) {
  return wrapper
    .findAll('[data-pref]')
    .find((row) => row.attributes('data-pref') === text)
    ?.find('[role="switch"]')
}

function batteryBox(wrapper: ReturnType<typeof mountInApp>) {
  return toggle(wrapper, t('settings.aiOnBattery'))
}

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
    const selector = `[data-pref="${t('settings.videos')}"] [role="switch"]`
    const first = mountSettings()
    const firstBox = key === 'videos' ? first.get(selector) : batteryBox(first)
    if (!firstBox) throw new Error('missing settings checkbox')
    await firstBox.trigger('click')
    first.unmount()
    const next = mountSettings()
    try {
      const nextBox = key === 'videos' ? next.get(selector) : batteryBox(next)
      if (!nextBox) throw new Error('missing settings checkbox')
      await nextBox.trigger('click')
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

it.each(['videos', 'aiOnBattery', 'idleOnly'] as const)(
  'rolls back the latest failed %s save to the last persisted value',
  async (key) => {
    setActivePinia(createPinia())
    const lib = useLibrary()
    const saves = {
      videos: [engine.setVideos, lib.saveVideos],
      aiOnBattery: [engine.setAIOnBattery, lib.saveAIOnBattery],
      idleOnly: [engine.setIdleOnly, lib.saveIdleOnly],
    } as const
    const [engineSave, set] = saves[key]
    vi.mocked(engineSave).mockResolvedValueOnce().mockRejectedValueOnce(new Error('latest toggle failed'))
    await set(true)
    await set(false)
    expect(lib.settings[key]).toBe(true)
    expect(lib.settingsError).toContain('latest toggle failed')
  },
)

it('gives way to full-screen apps and a busy PC by default, and saves each switch', async () => {
  setActivePinia(createPinia())
  const wrapper = mountSettings()
  try {
    const fullScreen = toggle(wrapper, t('settings.pauseFullScreen'))
    const busy = toggle(wrapper, t('settings.pauseBusy'))
    const idle = toggle(wrapper, t('settings.idleOnly'))
    if (!fullScreen || !busy || !idle) throw new Error('missing a background work switch')
    expect([fullScreen, busy, idle].map((box) => box.attributes('aria-checked') === 'true')).toEqual([
      true,
      true,
      false,
    ])
    expect(wrapper.text()).toContain(t('settings.idleOnlyHint', { n: AWAY_MINUTES }))
    await fullScreen.trigger('click')
    await busy.trigger('click')
    await idle.trigger('click')
    await flushPromises()
    expect(engine.setPauseFullScreen).toHaveBeenCalledExactlyOnceWith(false)
    expect(engine.setPauseBusy).toHaveBeenCalledExactlyOnceWith(false)
    expect(engine.setIdleOnly).toHaveBeenCalledExactlyOnceWith(true)
  } finally {
    wrapper.unmount()
  }
})

it('keeps a newer slider input when the previous save fails before its debounce fires', async () => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  const lib = useLibrary()
  let fail: (reason: Error) => void = () => {
    throw new Error('the save has not started')
  }
  vi.mocked(engine.setTagConfidence)
    .mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
    .mockResolvedValueOnce()
  const wrapper = mountSettings()
  try {
    await sliderInput(wrapper, 'confidence', 70)
    await vi.advanceTimersByTimeAsync(250)
    await sliderInput(wrapper, 'confidence', 90)
    const newest = lib.settings.tagConfidence
    fail(new Error('older confidence failed'))
    await flushPromises()
    expect(lib.settings.tagConfidence).toBe(newest)
    expect(wrapper.get('#confidence [role="slider"]').attributes('aria-valuenow')).toBe('90')
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
  let finish: () => void = () => {
    throw new Error('the save has not started')
  }
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
  const first = mountSettings()
  await first.get('.event-gap input').setValue('24')
  await first.get('.event-gap input').setValue('36')
  first.unmount()
  const next = mountSettings()
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
  let finish: () => void = () => {
    throw new Error('the save has not started')
  }
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
  const first = mountSettings()
  await sliderInput(first, 'confidence', 50)
  await vi.advanceTimersByTimeAsync(250)
  await sliderInput(first, 'confidence', 60)
  first.unmount()
  const next = mountSettings()
  try {
    await sliderInput(next, 'confidence', 80)
    const newest = lib.settings.tagConfidence
    await vi.advanceTimersByTimeAsync(250)
    expect(writes).toHaveLength(1)
    finish()
    await flushPromises()
    expect(writes).toHaveLength(2)
    expect(writes[1]).toBe(newest)
    expect(persisted).toBe(newest)
    expect(next.get('#confidence [role="slider"]').attributes('aria-valuenow')).toBe('80')
  } finally {
    next.unmount()
  }
})

it('persists the final slider value when leaving settings before the debounce', async () => {
  setActivePinia(createPinia())
  vi.spyOn(useLibrary(), 'refresh').mockResolvedValue()
  const wrapper = mountSettings()
  await sliderInput(wrapper, 'confidence', 60)
  const value = useLibrary().settings.tagConfidence
  expect(engine.setTagConfidence).not.toHaveBeenCalled()
  wrapper.unmount()
  await flushPromises()
  expect(engine.setTagConfidence).toHaveBeenCalledExactlyOnceWith(value)
})

it('puts the videos checkbox back when the engine refuses', async () => {
  setActivePinia(createPinia())
  vi.mocked(engine.setVideos).mockRejectedValueOnce(new Error('database is locked'))
  const wrapper = mountSettings()
  const box = wrapper.get<HTMLButtonElement>(`[data-pref="${t('settings.videos')}"] [role="switch"]`)
  await box.trigger('click')
  await flushPromises()
  expect(useLibrary().settings.videos).toBe(false)
  expect(box.attributes('aria-checked')).toBe('false')
  expect(wrapper.text()).toContain('database is locked')
  wrapper.unmount()
})

it('serializes gap writes and ignores a superseded failure', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  vi.spyOn(lib, 'refresh').mockResolvedValue()
  let fail: (reason: Error) => void = () => {
    throw new Error('the save has not started')
  }
  vi.mocked(engine.setEventGapHours)
    .mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
    .mockResolvedValueOnce()
  const wrapper = mountSettings()
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
  let finish: () => void = () => {
    throw new Error('the save has not started')
  }
  vi.mocked(engine.setEventGapHours)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockRejectedValueOnce(new Error('latest save failed'))
  const wrapper = mountSettings()
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
  let finish: () => void = () => {
    throw new Error('the save has not started')
  }
  vi.mocked(engine.setEventGapHours)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockResolvedValueOnce()
  const wrapper = mountSettings()
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
  const wrapper = mountSettings()
  const input = wrapper.get<HTMLInputElement>('.event-gap input')
  await input.setValue(entered)
  await flushPromises()
  expect(lib.settings.eventGapHours).toBe(expected)
  expect(input.element.value).toBe(String(expected))
  expect(engine.setEventGapHours).toHaveBeenCalledExactlyOnceWith(expected)
  expect(wrapper.text()).toContain(t('settings.eventGapRange', { min: 1, max: 72, n: expected }))
  wrapper.unmount()
})

it('shows the thumbnail cache cap in GB, 2 GB by default, and how much is in use', async () => {
  setActivePinia(createPinia())
  vi.mocked(engine.thumbCacheBytes).mockResolvedValue(1.5 * 1024 ** 3)
  const wrapper = mountSettings()
  await flushPromises()
  expect(wrapper.get<HTMLInputElement>('.cache-cap input').element.value).toBe('2')
  expect(wrapper.get('.cache-used').text()).toContain('1.5')
  expect(wrapper.get('.cache-used').text()).toContain('GB')
  wrapper.unmount()
})

it('saves a new cache cap in MB through the library store', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  const wrapper = mountSettings()
  await wrapper.get('.cache-cap input').setValue('4.5')
  await flushPromises()
  expect(engine.setThumbCacheMB).toHaveBeenCalledExactlyOnceWith(4608)
  expect(lib.settings.thumbCacheMB).toBe(4608)
  wrapper.unmount()
})

it.each([
  ['', THUMB_CACHE_DEFAULT_MB, '2'],
  ['0.1', THUMB_CACHE_MIN_MB, '0.25'],
  ['500', THUMB_CACHE_MAX_MB, '100'],
] as const)('holds the cache cap typed as %s to its range', async (entered, expectedMB, shown) => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  const wrapper = mountSettings()
  const input = wrapper.get<HTMLInputElement>('.cache-cap input')
  await input.setValue(entered)
  await flushPromises()
  expect(lib.settings.thumbCacheMB).toBe(expectedMB)
  expect(input.element.value).toBe(shown)
  expect(wrapper.text()).toContain(t('settings.cacheRange', { min: 0.25, max: 100, n: Number(shown) }))
  wrapper.unmount()
})

it('puts the cache cap back and says why when the engine refuses it', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  vi.mocked(engine.setThumbCacheMB).mockRejectedValueOnce(new Error('settings are locked'))
  const wrapper = mountSettings()
  const input = wrapper.get<HTMLInputElement>('.cache-cap input')
  await input.setValue('8')
  await flushPromises()
  expect(lib.settings.thumbCacheMB).toBe(THUMB_CACHE_DEFAULT_MB)
  expect(input.element.value).toBe('2')
  expect(wrapper.text()).toContain('settings are locked')
  wrapper.unmount()
})

it('saves the newest cache cap after an older one, and ignores the older failure', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  let fail: (reason: Error) => void = () => {
    throw new Error('the save has not started')
  }
  vi.mocked(engine.setThumbCacheMB)
    .mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
    .mockResolvedValueOnce()
  const wrapper = mountSettings()
  const input = wrapper.get<HTMLInputElement>('.cache-cap input')
  await input.setValue('3')
  await input.setValue('5')
  expect(engine.setThumbCacheMB).toHaveBeenCalledExactlyOnceWith(3072)
  fail(new Error('older cap failed'))
  await flushPromises()
  expect(engine.setThumbCacheMB).toHaveBeenNthCalledWith(2, 5120)
  expect(lib.settings.thumbCacheMB).toBe(5120)
  expect(wrapper.text()).not.toContain('older cap failed')
  wrapper.unmount()
})

// The library store as the engine's settings leave it on a 20-thread machine.
function threadsLibrary() {
  setActivePinia(createPinia())
  const lib = useLibrary()
  Object.assign(lib.settings, { cpuThreads: 10, maxCPUThreads: 20, defaultCPUThreads: 10 })
  return lib
}

it('shows no thread setting until the machine’s threads are known', () => {
  setActivePinia(createPinia())
  const wrapper = mountSettings()
  expect(wrapper.find('#cpu-threads').exists()).toBe(false)
  wrapper.unmount()
})

it('shows the indexing threads out of the machine’s, following the slider, and saves them when let go', async () => {
  const lib = threadsLibrary()
  const wrapper = mountSettings()
  const slider = wrapper.get('#cpu-threads [role="slider"]')
  expect(slider.attributes('aria-valuemax')).toBe('20')
  expect(wrapper.text()).toContain(t('settings.threads', { n: 10, max: 20 }))
  expect(wrapper.text()).toContain(t('settings.threadsHint', { n: 10 }))
  await sliderInput(wrapper, 'cpu-threads', 16)
  expect(wrapper.text()).toContain(t('settings.threads', { n: 16, max: 20 }))
  expect(engine.setCPUThreads).not.toHaveBeenCalled()
  await sliderCommit(wrapper, 'cpu-threads')
  await flushPromises()
  expect(engine.setCPUThreads).toHaveBeenCalledExactlyOnceWith(16)
  expect(lib.settings.cpuThreads).toBe(16)
  wrapper.unmount()
})

it('puts the indexing threads back and says why when the engine refuses them', async () => {
  const lib = threadsLibrary()
  vi.mocked(engine.setCPUThreads).mockRejectedValueOnce(new Error('settings are locked'))
  const wrapper = mountSettings()
  await sliderInput(wrapper, 'cpu-threads', 4)
  await sliderCommit(wrapper, 'cpu-threads')
  await flushPromises()
  expect(lib.settings.cpuThreads).toBe(10)
  expect(wrapper.text()).toContain(t('settings.threads', { n: 10, max: 20 }))
  expect(wrapper.text()).toContain('settings are locked')
  wrapper.unmount()
})

it('reads the cache size again while the page is open, and stops after it closes', async () => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  vi.mocked(engine.thumbCacheBytes)
    .mockResolvedValueOnce(1024 ** 3)
    .mockResolvedValue(2 * 1024 ** 3)
  const wrapper = mountSettings()
  await flushPromises()
  expect(wrapper.get('.cache-used').text()).toContain('1')
  await vi.advanceTimersByTimeAsync(5000)
  expect(wrapper.get('.cache-used').text()).toContain('2')
  wrapper.unmount()
  const calls = vi.mocked(engine.thumbCacheBytes).mock.calls.length
  await vi.advanceTimersByTimeAsync(20000)
  expect(vi.mocked(engine.thumbCacheBytes)).toHaveBeenCalledTimes(calls)
})

it('offers the real theme, six named accents and two densities without enum zero values', async () => {
  setActivePinia(createPinia())
  const wrapper = mountSettings()
  try {
    expect(wrapper.get('[data-appearance="theme"]').findAll('[role="radio"]')).toHaveLength(3)
    expect(
      wrapper
        .get('[data-appearance="accent"]')
        .findAll('label')
        .map((label) => label.text()),
    ).toEqual(['Blue', 'Teal', 'Green', 'Violet', 'Rose', 'Amber'])
    expect(wrapper.get('[data-appearance="density"]').findAll('[role="radio"]')).toHaveLength(2)
    await chooseAppearance(wrapper, 'theme', 2)
    await flushPromises()
    expect(usePreferences().prefs.theme).toBe(Theme.ThemeDark)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    await chooseAppearance(wrapper, 'accent', 1)
    await flushPromises()
    expect(usePreferences().prefs.accent).toBe(Accent.AccentTeal)
    expect(document.documentElement.dataset.accent).toBe(Accent.AccentTeal)
    await chooseAppearance(wrapper, 'density', 1)
    await flushPromises()
    expect(usePreferences().prefs.density).toBe(Density.DensityCompact)
    expect(document.documentElement.dataset.density).toBe(Density.DensityCompact)
    expect(engine.setPreferences).toHaveBeenLastCalledWith(
      expect.objectContaining({ theme: Theme.ThemeDark, accent: Accent.AccentTeal, density: Density.DensityCompact }),
    )
  } finally {
    wrapper.unmount()
  }
})

it('uses the text scale bounds and step and restores the default with its reset', async () => {
  setActivePinia(createPinia())
  const wrapper = mountSettings()
  try {
    const slider = wrapper
      .findAllComponents(USlider)
      .find((component) => component.attributes('data-appearance') === 'textScale')
    if (!slider) throw new Error('missing text size slider')
    expect(slider.props()).toEqual(expect.objectContaining({ min: TEXT_SCALE_MIN, max: TEXT_SCALE_MAX, step: 5 }))
    const reset = wrapper.findAll('button').find((button) => button.text() === t('settings.resetText'))
    if (!reset) throw new Error('missing text size reset')
    expect(reset.attributes('disabled')).toBeDefined()
    slider.vm.$emit('update:modelValue', TEXT_SCALE_DEFAULT + 5)
    await flushPromises()
    expect(usePreferences().prefs.textScale).toBe(TEXT_SCALE_DEFAULT + 5)
    expect(engine.setPreferences).toHaveBeenLastCalledWith(
      expect.objectContaining({ textScale: TEXT_SCALE_DEFAULT + 5 }),
    )
    await reset.trigger('click')
    await flushPromises()
    expect(usePreferences().prefs.textScale).toBe(TEXT_SCALE_DEFAULT)
    expect(engine.setPreferences).toHaveBeenLastCalledWith(expect.objectContaining({ textScale: TEXT_SCALE_DEFAULT }))
  } finally {
    wrapper.unmount()
  }
})

it('applies appearance at once, shows saving, queues newer choices and rolls a failed latest save back', async () => {
  setActivePinia(createPinia())
  await usePreferences().load()
  const notifications = recordNotifications()
  let finish: () => void = () => {
    throw new Error('the appearance save has not started')
  }
  vi.mocked(engine.setPreferences)
    .mockImplementationOnce(
      (prefs) =>
        new Promise<Bridge.Preferences>((resolve) => {
          finish = () => resolve(prefs)
        }),
    )
    .mockRejectedValueOnce(new Error('appearance is locked'))
  const wrapper = mountSettings()
  try {
    await flushPromises()
    await chooseAppearance(wrapper, 'accent', 1)
    expect(usePreferences().prefs.accent).toBe(Accent.AccentTeal)
    expect(wrapper.get('[role="status"]').text()).toBe(t('settings.saving'))
    await chooseAppearance(wrapper, 'accent', 3)
    expect(usePreferences().prefs.accent).toBe(Accent.AccentViolet)
    expect(engine.setPreferences).toHaveBeenCalledTimes(1)
    finish()
    await flushPromises()
    expect(engine.setPreferences).toHaveBeenCalledTimes(2)
    expect(engine.setPreferences).toHaveBeenNthCalledWith(2, expect.objectContaining({ accent: Accent.AccentViolet }))
    expect(usePreferences().prefs.accent).toBe(Accent.AccentTeal)
    expect(document.documentElement.dataset.accent).toBe(Accent.AccentTeal)
    expect(usePreferences().saving).toBe(false)
    expect(notifications.shown.some((notification) => notification.title.includes('appearance is locked'))).toBe(true)
  } finally {
    wrapper.unmount()
    notifications.stop()
  }
})

it('filters by descriptions and categories and recovers from a search with no matches', async () => {
  setActivePinia(createPinia())
  const wrapper = mountSettings()
  try {
    await wrapper.get('input[type="search"]').setValue('processor busy')
    expect(
      wrapper
        .findAll('[data-pref]')
        .filter((row) => row.isVisible())
        .map((row) => row.attributes('data-pref')),
    ).toEqual([t('settings.pauseBusy')])
    const appearance = wrapper
      .get('nav')
      .findAll('button')
      .find((button) => button.text() === t('settings.appearance'))
    if (!appearance) throw new Error('missing appearance category')
    await appearance.trigger('click')
    expect(wrapper.text()).toContain(t('settings.noResults'))
    const reset = wrapper.findAll('button').find((button) => button.text() === t('settings.clearSearch'))
    if (!reset) throw new Error('missing clear search action')
    await reset.trigger('click')
    expect(wrapper.findAll('[data-category]')).toHaveLength(7)
    expect(
      wrapper.get('input[type="search"]').element instanceof HTMLInputElement &&
        wrapper.get<HTMLInputElement>('input[type="search"]').element.value,
    ).toBe('')
  } finally {
    wrapper.unmount()
  }
})

it('keeps an XMP export busy and retains its result while settings are filtered', async () => {
  setActivePinia(createPinia())
  useLibrary().settings.xmp = true
  let finish: (result: Bridge.XMPExport) => void = () => {
    throw new Error('the export has not started')
  }
  vi.mocked(engine.exportXMPNow).mockReturnValueOnce(
    new Promise<Bridge.XMPExport>((resolve) => {
      finish = resolve
    }),
  )
  const wrapper = mountSettings()
  try {
    await wrapper.get('[data-xmp="export"]').trigger('click')
    expect(wrapper.get('[data-xmp="export"]').attributes('disabled')).toBeDefined()
    await wrapper.get('input[type="search"]').setValue('density')
    expect(wrapper.find('[data-xmp="export"]').exists()).toBe(false)
    await wrapper.get('input[type="search"]').setValue('')
    expect(wrapper.get('[data-xmp="export"]').attributes('disabled')).toBeDefined()
    expect(engine.exportXMPNow).toHaveBeenCalledOnce()
    finish({ written: 12, unchanged: 0, skipped: 0 })
    await flushPromises()
    expect(wrapper.get('[data-xmp="export"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('Wrote 12, 0 already up to date, 0 left alone.')
  } finally {
    wrapper.unmount()
  }
})

it('retains vertical arrow selection and focus in the theme radio group', async () => {
  setActivePinia(createPinia())
  const wrapper = mountSettings()
  try {
    const group = wrapper.get('[data-appearance="theme"]')
    await group.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    expect(usePreferences().prefs.theme).toBe(Theme.ThemeLight)
    expect(document.activeElement).toBe(group.get('[aria-checked="true"]').element)
    await group.trigger('keydown', { key: 'ArrowUp' })
    await flushPromises()
    expect(usePreferences().prefs.theme).toBe(Theme.ThemeSystem)
    expect(document.activeElement).toBe(group.get('[aria-checked="true"]').element)
  } finally {
    wrapper.unmount()
  }
})
