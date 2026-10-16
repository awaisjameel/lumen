import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import USelect from '@nuxt/ui/components/Select.vue'
import { mountInApp, recordNotifications } from '../shared/testing'
import type { RecordedNotifications } from '../shared/testing'
import { defaultPreferences, usePreferences } from '../shared/preferences'
import { useLibrary } from '../library/store'
import { StartView, ViewerWheel, CompressionEffort, Family, engine } from '../bridge'
import type * as Bridge from '../bridge'
import { t } from '../shared/i18n'
import { shortcutCatalogue } from '../shared/shortcuts'
import LibrarySettings from './LibrarySettings.vue'
import ViewingSettings from './ViewingSettings.vue'
import VideoSettings from './VideoSettings.vue'
import CompressionSettings from './CompressionSettings.vue'
import FormatSettings from './FormatSettings.vue'
import OcrSetting from './OcrSetting.vue'
import XmpSetting from './XmpSetting.vue'
import ShortcutSettings from './ShortcutSettings.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    migratePreferences: vi.fn<typeof Bridge.engine.migratePreferences>(() => Promise.resolve(defaultPreferences())),
    setPreferences: vi.fn<typeof Bridge.engine.setPreferences>((prefs) => Promise.resolve(prefs)),
  },
}))

let notifications: RecordedNotifications
beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
  notifications = recordNotifications()
})
afterEach(() => notifications.stop())

function show(component: Parameters<typeof mountInApp>[0], query = '') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }],
  })
  return mountInApp(component, { props: { query }, plugins: [router] })
}

it.each([
  {
    component: LibrarySettings,
    label: 'settings.thumbFit',
    change: () => usePreferences().set({ startView: StartView.StartPeople }),
    changedLabel: 'settings.startView',
  },
  {
    component: ViewingSettings,
    label: 'viewer.wheel',
    change: () => usePreferences().setViewer({ wheel: ViewerWheel.ViewerWheelNavigate }),
    changedLabel: 'viewer.wheel',
  },
  {
    component: VideoSettings,
    label: 'viewer.volume',
    change: () => usePreferences().setViewer({ volume: 0.5 }),
    changedLabel: 'viewer.volume',
  },
  {
    component: CompressionSettings,
    label: 'compression.effort',
    change: () => usePreferences().setCompression({ effort: CompressionEffort.CompressMaximum }),
    changedLabel: 'compression.effort',
  },
] as const)('marks a changed setting in $changedLabel', async ({ component, label, changedLabel, change }) => {
  const wrapper = show(component)
  try {
    expect(
      wrapper
        .get(`[data-pref="${t(label)}"]`)
        .find(`[aria-label="${t('settings.modified')}"]`)
        .exists(),
    ).toBe(false)
    await change()
    await flushPromises()
    expect(
      wrapper
        .get(`[data-pref="${t(changedLabel)}"]`)
        .find(`[aria-label="${t('settings.modified')}"]`)
        .exists(),
    ).toBe(true)
  } finally {
    wrapper.unmount()
  }
})

it.each([
  {
    component: FormatSettings,
    change: () => {
      useLibrary().settings.excludedFormats = [Family.FamilyPNG]
    },
  },
  {
    component: OcrSetting,
    change: () => {
      useLibrary().settings.ocr = true
    },
  },
  {
    component: XmpSetting,
    change: () => {
      useLibrary().settings.xmp = true
    },
  },
])('marks an enabled engine preference as modified', async ({ component, change }) => {
  const wrapper = show(component)
  try {
    expect(wrapper.find(`[aria-label="${t('settings.modified')}"]`).exists()).toBe(false)
    change()
    await flushPromises()
    expect(wrapper.find(`[aria-label="${t('settings.modified')}"]`).exists()).toBe(true)
  } finally {
    wrapper.unmount()
  }
})

it('offers Library as a start destination and excludes the retired Videos destination', () => {
  const wrapper = show(LibrarySettings)
  try {
    const select = wrapper.getComponent(USelect)
    expect(select.props()).toMatchObject({
      items: expect.arrayContaining([expect.objectContaining({ value: StartView.StartLibrary })]),
    })
    expect(select.props()).not.toMatchObject({
      items: expect.arrayContaining([expect.objectContaining({ value: StartView.StartVideos })]),
    })
    expect(wrapper.get('a').attributes('href')).toBe('/')
  } finally {
    wrapper.unmount()
  }
})

it('finds individual shortcut actions and keeps their changed marker', async () => {
  const entry = shortcutCatalogue.find((entry) => entry.id === 'viewer.compare')
  if (!entry) throw new Error('missing compare shortcut')
  await usePreferences().setShortcut(entry.id, 'Ctrl+Alt+Q')
  const wrapper = show(ShortcutSettings, t(entry.title))
  try {
    expect(wrapper.findAll('[data-shortcut]').map((row) => row.attributes('data-shortcut'))).toEqual([entry.id])
    expect(
      wrapper
        .get(`[data-shortcut="${entry.id}"]`)
        .find(`[aria-label="${t('settings.modified')}"]`)
        .exists(),
    ).toBe(true)
    expect(engine.setPreferences).toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})
