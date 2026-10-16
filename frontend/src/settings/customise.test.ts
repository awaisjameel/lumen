import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { mountInApp, recordNotifications } from '../shared/testing'
import { engine, ErrorCode, ExportFormat, Family, FocusRing, Motion, PrefCategory, Theme } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import { setShortcutOverrides } from '../shared/commands'
import { confirm } from '../shared/confirm'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { defaultPreferences, usePreferences } from '../shared/preferences'
import CustomisationSettings from './CustomisationSettings.vue'
import ExportPresets from './ExportPresets.vue'
import FormatSettings from './FormatSettings.vue'
import ShortcutSettings from './ShortcutSettings.vue'

vi.mock('../shared/confirm', () => ({ confirm: vi.fn<typeof confirm>() }))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    migratePreferences: vi.fn<typeof Bridge.engine.migratePreferences>(),
    preferences: vi.fn<typeof Bridge.engine.preferences>(),
    setPreferences: vi.fn<typeof Bridge.engine.setPreferences>(),
    resetPreferences: vi.fn<typeof Bridge.engine.resetPreferences>(),
    exportCustomisation: vi.fn<typeof Bridge.engine.exportCustomisation>(),
    readCustomisation: vi.fn<typeof Bridge.engine.readCustomisation>(),
    setFormatExcluded: vi.fn<typeof Bridge.engine.setFormatExcluded>(),
    settings: vi.fn<typeof Bridge.engine.settings>(),
    timeline: vi.fn<typeof Bridge.engine.timeline>().mockResolvedValue([]),
  },
  logUIError: vi.fn<typeof Bridge.logUIError>(),
}))

const settings: Bridge.Settings = {
  videos: true,
  aiOnBattery: false,
  tagConfidence: 0.02,
  eventGapHours: 12,
  thumbCacheMB: 2048,
  ocr: false,
  xmp: false,
  cpuThreads: 4,
  maxCPUThreads: 8,
  defaultCPUThreads: 4,
  pauseFullScreen: true,
  pauseBusy: true,
  idleOnly: false,
  excludedFormats: [],
}

let mounted: ReturnType<typeof mountInApp>[] = []
function show(component: Parameters<typeof mountInApp>[0]) {
  const wrapper = mountInApp(component)
  mounted.push(wrapper)
  return wrapper
}

beforeEach(async () => {
  setActivePinia(createPinia())
  localStorage.clear()
  vi.mocked(confirm).mockReset().mockResolvedValue(true)
  vi.mocked(engine.migratePreferences).mockReset().mockResolvedValue(defaultPreferences())
  vi.mocked(engine.preferences).mockReset().mockResolvedValue(defaultPreferences())
  vi.mocked(engine.setPreferences)
    .mockReset()
    .mockImplementation((p) => Promise.resolve(p))
  vi.mocked(engine.resetPreferences).mockReset().mockResolvedValue()
  vi.mocked(engine.exportCustomisation).mockReset().mockResolvedValue('')
  vi.mocked(engine.readCustomisation).mockReset()
  vi.mocked(engine.setFormatExcluded).mockReset().mockResolvedValue()
  vi.mocked(engine.settings)
    .mockReset()
    .mockImplementation(() => Promise.resolve({ ...settings, excludedFormats: [...settings.excludedFormats] }))
  await usePreferences().load()
})
afterEach(() => {
  for (const wrapper of mounted) wrapper.unmount()
  mounted = []
  setShortcutOverrides({})
  document.body.innerHTML = ''
})

function row(wrapper: ReturnType<typeof mountInApp>, id: string) {
  return wrapper.get(`[data-shortcut="${id}"]`)
}
async function record(wrapper: ReturnType<typeof mountInApp>, id: string, init: KeyboardEventInit) {
  const change = row(wrapper, id).get('[data-shortcut-change]')
  await change.trigger('click')
  expect(change.attributes('aria-pressed')).toBe('true')
  await change.trigger('keydown', init)
  await flushPromises()
}

it('gives an action the keys pressed, saves them, and can give its own back', async () => {
  const prefs = usePreferences()
  const wrapper = show(ShortcutSettings)
  expect(row(wrapper, 'viewer.zoomIn').get('[data-shortcut-keys]').text()).toBe('=')
  expect(row(wrapper, 'viewer.rotate').get('[data-shortcut-keys]').text()).toBe(t('settings.shortcutNone'))
  expect(row(wrapper, 'viewer.zoomIn').find('[data-shortcut-reset]').exists()).toBe(false)

  await record(wrapper, 'viewer.rotate', { key: 'r' })
  expect(prefs.prefs.shortcuts).toEqual({ 'viewer.rotate': 'R' })
  expect(engine.setPreferences).toHaveBeenLastCalledWith(
    expect.objectContaining({ shortcuts: { 'viewer.rotate': 'R' } }),
  )
  expect(row(wrapper, 'viewer.rotate').get('[data-shortcut-keys]').text()).toBe('R')
  expect(row(wrapper, 'viewer.rotate').get('[role="status"]').text()).toBe(
    t('settings.shortcutSet', { action: t('viewer.rotate'), keys: 'R' }),
  )

  await row(wrapper, 'viewer.rotate').get('[data-shortcut-reset]').trigger('click')
  await flushPromises()
  expect(prefs.prefs.shortcuts).toEqual({})
  expect(row(wrapper, 'viewer.rotate').get('[data-shortcut-keys]').text()).toBe(t('settings.shortcutNone'))

  // Pressing an action's own keys is no change to save.
  await record(wrapper, 'viewer.zoomIn', { key: '=' })
  expect(prefs.prefs.shortcuts).toEqual({})
})

it('refuses keys that already belong to an action that would hear them, and says whose', async () => {
  const prefs = usePreferences()
  const wrapper = show(ShortcutSettings)
  await record(wrapper, 'viewer.rotate', { key: 'f' })
  expect(prefs.prefs.shortcuts).toEqual({})
  expect(row(wrapper, 'viewer.rotate').get('[role="alert"]').text()).toBe(
    t('settings.shortcutTaken', { keys: 'F', action: t('details.favourite') }),
  )
  // The grid may use a key the viewer uses.
  await record(wrapper, 'library.files', { key: 'ArrowLeft', shiftKey: true })
  expect(prefs.prefs.shortcuts).toEqual({ 'library.files': 'Shift+ArrowLeft' })
})

it('never assigns Escape or Tab, and leaves the viewer’s Close fixed', async () => {
  const prefs = usePreferences()
  const wrapper = show(ShortcutSettings)
  expect(row(wrapper, 'viewer.close').find('[data-shortcut-change]').exists()).toBe(false)
  expect(row(wrapper, 'viewer.close').text()).toContain(t('settings.shortcutLocked'))

  // Escape ends the recording without assigning anything.
  const change = row(wrapper, 'viewer.rotate').get('[data-shortcut-change]')
  await change.trigger('click')
  await change.trigger('keydown', { key: 'Escape' })
  expect(change.attributes('aria-pressed')).toBe('false')
  await record(wrapper, 'viewer.rotate', { key: '+' })
  expect(row(wrapper, 'viewer.rotate').get('[role="alert"]').text()).toBe(t('settings.shortcutReserved'))
  // Modifiers alone keep the recording going.
  await change.trigger('click')
  await change.trigger('keydown', { key: 'Control', ctrlKey: true })
  expect(change.attributes('aria-pressed')).toBe('true')
  expect(prefs.prefs.shortcuts).toEqual({})
})

it('removes a shortcut, and resets every changed one at once', async () => {
  const prefs = usePreferences()
  const wrapper = show(ShortcutSettings)
  expect(wrapper.get('[data-shortcut-reset-all]').attributes('disabled')).toBeDefined()
  await row(wrapper, 'viewer.zoomIn').get('[data-shortcut-remove]').trigger('click')
  await flushPromises()
  expect(prefs.prefs.shortcuts).toEqual({ 'viewer.zoomIn': '' })
  expect(row(wrapper, 'viewer.zoomIn').get('[data-shortcut-keys]').text()).toBe(t('settings.shortcutNone'))
  // The keys it gave up are free for another action.
  await record(wrapper, 'viewer.rotate', { key: '=' })
  expect(prefs.prefs.shortcuts).toEqual({ 'viewer.zoomIn': '', 'viewer.rotate': '=' })

  await wrapper.get('[data-shortcut-reset-all]').trigger('click')
  await flushPromises()
  expect(prefs.prefs.shortcuts).toEqual({})
  expect(row(wrapper, 'viewer.zoomIn').get('[data-shortcut-keys]').text()).toBe('=')
})

it('shows what a customisation file would change, and applies it only when asked', async () => {
  const prefs = usePreferences()
  const file = {
    ...defaultPreferences(),
    theme: Theme.ThemeDark,
    hideVideoBadge: true,
    hideDurationBadge: true,
    shortcuts: { 'viewer.rotate': 'R' },
  }
  vi.mocked(engine.readCustomisation).mockResolvedValue({
    name: 'Lumen customisation.json',
    preferences: file,
    changed: ['theme', 'hideVideoBadge', 'hideDurationBadge', 'shortcuts'],
  })
  const wrapper = show(CustomisationSettings)
  await wrapper.get('[data-preset-import]').trigger('click')
  await flushPromises()

  const changes = [...document.body.querySelectorAll('[data-preset-changes] li')].map((li) => li.textContent)
  // The two badges are one setting on screen.
  expect(changes).toEqual([t('settings.pref.theme'), t('settings.badges'), t('settings.shortcuts')])
  expect(document.body.textContent).toContain(t('settings.presetPreviewTitle', { name: 'Lumen customisation.json' }))
  expect(engine.setPreferences).not.toHaveBeenCalled()
  expect(prefs.prefs.theme).toBe(Theme.ThemeSystem)

  const apply = document.body.querySelector<HTMLElement>('[data-preset-apply]')
  if (!apply) throw new Error('missing Apply')
  apply.click()
  await flushPromises()
  expect(engine.setPreferences).toHaveBeenCalledExactlyOnceWith(file)
  expect(prefs.prefs).toEqual(file)
  expect(document.documentElement.hasAttribute('data-hide-video')).toBe(true)
  expect(wrapper.text()).toContain(t('settings.presetApplied'))
})

it('applies nothing when the file is declined, cancelled or refused', async () => {
  const wrapper = show(CustomisationSettings)
  // The file dialog was closed: no preview opens.
  vi.mocked(engine.readCustomisation).mockResolvedValueOnce({
    name: '',
    preferences: defaultPreferences(),
    changed: [],
  })
  await wrapper.get('[data-preset-import]').trigger('click')
  await flushPromises()
  expect(document.body.querySelector('[data-preset-apply]')).toBeNull()

  // A file that matches offers nothing to apply.
  vi.mocked(engine.readCustomisation).mockResolvedValueOnce({
    name: 'same.json',
    preferences: defaultPreferences(),
    changed: [],
  })
  await wrapper.get('[data-preset-import]').trigger('click')
  await flushPromises()
  expect(document.body.textContent).toContain(t('settings.presetPreviewNone'))
  expect(document.body.querySelector('[data-preset-apply]')).toBeNull()

  // A file that is not Lumen's is explained in the row.
  vi.mocked(engine.readCustomisation).mockRejectedValueOnce(
    new Error('refused', { cause: { code: ErrorCode.PresetInvalid } }),
  )
  await wrapper.get('[data-preset-import]').trigger('click')
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toBe(t('error.presetInvalid'))
  expect(engine.setPreferences).not.toHaveBeenCalled()
})

it('says where the customisation was saved, and nothing when saving was cancelled', async () => {
  const wrapper = show(CustomisationSettings)
  await wrapper.get('[data-preset-export]').trigger('click')
  await flushPromises()
  expect(wrapper.text()).not.toContain('Saved as')
  vi.mocked(engine.exportCustomisation).mockResolvedValueOnce('mine.json')
  await wrapper.get('[data-preset-export]').trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain(t('settings.presetExported', { name: 'mine.json' }))
})

it('keeps the import preview available without reporting success when its save fails', async () => {
  const prefs = usePreferences()
  const wrapper = show(CustomisationSettings)
  vi.mocked(engine.readCustomisation).mockResolvedValueOnce({
    name: 'mine.json',
    preferences: { ...defaultPreferences(), theme: Theme.ThemeDark },
    changed: ['theme'],
  })
  vi.mocked(engine.setPreferences).mockRejectedValueOnce(new Error('Disk full'))
  await wrapper.get('[data-preset-import]').trigger('click')
  await flushPromises()
  const apply = document.body.querySelector<HTMLElement>('[data-preset-apply]')
  if (!apply) throw new Error('missing Apply')
  apply.click()
  await flushPromises()
  expect(prefs.prefs.theme).toBe(Theme.ThemeSystem)
  expect(wrapper.text()).not.toContain(t('settings.presetApplied'))
  expect(document.body.querySelector('[data-preset-apply]')).not.toBeNull()
  document.body.querySelector<HTMLElement>('[data-preset-apply]')?.click()
  await flushPromises()
  expect(prefs.prefs.theme).toBe(Theme.ThemeDark)
  expect(wrapper.text()).toContain(t('settings.presetApplied'))
})

it('preserves a newer format choice when an older save fails, and says that it failed', async () => {
  const lib = useLibrary()
  lib.settings = { ...settings }
  let fail: ((error: Error) => void) | undefined
  vi.mocked(engine.setFormatExcluded).mockImplementationOnce(() => new Promise((_resolve, reject) => (fail = reject)))
  const first = lib.saveFormatExcluded(Family.FamilyRAW, true)
  const second = lib.saveFormatExcluded(Family.FamilyHEIF, true)
  await flushPromises()
  fail?.(new Error('Disk full'))
  await Promise.all([first, second])
  expect(lib.settings.excludedFormats).toEqual([Family.FamilyHEIF])
  // The formats are separate choices: the one that failed is still explained.
  expect(lib.settingErrors.excludedFormats).toBe(errorText(new Error('Disk full')))
})

it('can save a format again when both the previous save and its recovery read fail', async () => {
  const lib = useLibrary()
  lib.settings = { ...settings }
  vi.mocked(engine.setFormatExcluded).mockRejectedValueOnce(new Error('Disk full'))
  vi.mocked(engine.settings).mockRejectedValueOnce(new Error('Library unavailable'))
  await lib.saveFormatExcluded(Family.FamilyRAW, true).catch(() => undefined)
  await lib.saveFormatExcluded(Family.FamilyHEIF, true).catch(() => undefined)
  expect(engine.setFormatExcluded).toHaveBeenLastCalledWith(Family.FamilyHEIF, true)
  expect(lib.settings.excludedFormats).toEqual([Family.FamilyHEIF])
})

it('finishes pending format saves before a reset and applies later edits afterwards', async () => {
  const lib = useLibrary()
  lib.settings = { ...settings }
  let finishSave: (() => void) | undefined
  let finishReset: (() => void) | undefined
  vi.mocked(engine.setFormatExcluded).mockImplementationOnce(() => new Promise((resolve) => (finishSave = resolve)))
  vi.mocked(engine.resetPreferences).mockImplementationOnce(() => new Promise((resolve) => (finishReset = resolve)))
  const first = lib.saveFormatExcluded(Family.FamilyRAW, true)
  const reset = lib.resetPreferences([PrefCategory.PrefIndexing])
  const later = lib.saveFormatExcluded(Family.FamilyHEIF, true)
  await flushPromises()
  expect(engine.resetPreferences).not.toHaveBeenCalled()
  finishSave?.()
  await flushPromises()
  expect(engine.resetPreferences).toHaveBeenCalledOnce()
  expect(engine.setFormatExcluded).toHaveBeenCalledTimes(1)
  finishReset?.()
  await Promise.all([first, reset, later])
  expect(engine.setFormatExcluded).toHaveBeenLastCalledWith(Family.FamilyHEIF, true)
  expect(lib.settings.excludedFormats).toEqual([Family.FamilyHEIF])
})

it('applies a viewing edit after a reset without restoring values from before it', async () => {
  const prefs = usePreferences()
  await prefs.set({ viewer: { ...prefs.viewer, speed: 2, skipSeconds: 30 } })
  let finishReset: (() => void) | undefined
  vi.mocked(engine.resetPreferences).mockImplementationOnce(() => new Promise((resolve) => (finishReset = resolve)))
  const reset = prefs.reset([PrefCategory.PrefVideo])
  const edit = prefs.setViewer({ muted: true })
  await flushPromises()
  expect(engine.resetPreferences).toHaveBeenCalledOnce()
  finishReset?.()
  await Promise.all([reset, edit])
  expect(prefs.viewer).toEqual({ ...defaultPreferences().viewer, muted: true })
})

it('can save a later edit after a failed reset has refreshed partially changed preferences', async () => {
  const prefs = usePreferences()
  await prefs.set({ theme: Theme.ThemeDark })
  vi.mocked(engine.resetPreferences).mockRejectedValueOnce(new Error('Cache unavailable'))
  const reset = prefs.reset([PrefCategory.PrefAppearance, PrefCategory.PrefIndexing])
  const refused = expect(reset).rejects.toThrow('Cache unavailable')
  const edit = prefs.set({ focusRing: FocusRing.FocusStrong })
  await refused
  expect(await edit).toBe(true)
  expect(prefs.prefs.theme).toBe(Theme.ThemeSystem)
  expect(prefs.prefs.focusRing).toBe(FocusRing.FocusStrong)
})

it('refuses to restore a shortcut whose old keys have been reassigned', async () => {
  const prefs = usePreferences()
  const wrapper = show(ShortcutSettings)
  await record(wrapper, 'viewer.favourite', { key: 'x' })
  await record(wrapper, 'viewer.rotate', { key: 'f' })
  await row(wrapper, 'viewer.favourite').get('[data-shortcut-reset]').trigger('click')
  await flushPromises()
  expect(prefs.prefs.shortcuts).toEqual({ 'viewer.favourite': 'X', 'viewer.rotate': 'F' })
  expect(row(wrapper, 'viewer.favourite').get('[role="alert"]').text()).toContain(t('viewer.rotate'))
  await record(wrapper, 'viewer.rotate', { key: '/' })
  expect(row(wrapper, 'viewer.rotate').get('[role="alert"]').text()).toContain(t('search.command'))
})

it('resets every category only after the user confirms, then shows what the library holds', async () => {
  const prefs = usePreferences()
  const lib = useLibrary()
  await prefs.set({ theme: Theme.ThemeDark, motion: Motion.MotionReduced, focusRing: FocusRing.FocusStrong })
  lib.settings = { ...settings, pauseBusy: false }
  const wrapper = show(CustomisationSettings)

  vi.mocked(confirm).mockResolvedValueOnce(false)
  await wrapper.get('[data-reset-all]').trigger('click')
  await flushPromises()
  expect(engine.resetPreferences).not.toHaveBeenCalled()
  expect(prefs.prefs.theme).toBe(Theme.ThemeDark)

  await wrapper.get('[data-reset-all]').trigger('click')
  await flushPromises()
  expect(confirm).toHaveBeenLastCalledWith(
    t('settings.resetAllTitle'),
    t('settings.resetAllText'),
    t('settings.resetAll'),
    { danger: true },
  )
  expect(engine.resetPreferences).toHaveBeenCalledExactlyOnceWith([
    PrefCategory.PrefAppearance,
    PrefCategory.PrefLibrary,
    PrefCategory.PrefViewing,
    PrefCategory.PrefVideo,
    PrefCategory.PrefAccessibility,
    PrefCategory.PrefIndexing,
    PrefCategory.PrefSearch,
    PrefCategory.PrefFiles,
    PrefCategory.PrefCompression,
  ])
  expect(prefs.prefs).toEqual(defaultPreferences())
  expect(document.documentElement.dataset.focus).toBe('standard')
  expect(lib.settings.pauseBusy).toBe(true)
  expect(wrapper.text()).toContain(t('settings.resetAllDone'))
})

it('explains a reset the engine refused and leaves the settings as they were', async () => {
  const prefs = usePreferences()
  await prefs.set({ theme: Theme.ThemeDark })
  vi.mocked(engine.resetPreferences).mockRejectedValueOnce(new Error('The library is read-only'))
  vi.mocked(engine.preferences).mockResolvedValueOnce(prefs.prefs)
  const wrapper = show(CustomisationSettings)
  await wrapper.get('[data-reset-all]').trigger('click')
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('The library is read-only')
  expect(prefs.prefs.theme).toBe(Theme.ThemeDark)
})

it('turns a photo format off and on, and puts back what the library holds when a save fails', async () => {
  const lib = useLibrary()
  lib.settings = { ...settings }
  const wrapper = show(FormatSettings)
  const raw = wrapper.get('[data-format="raw"]')
  expect(raw.attributes('aria-checked')).toBe('true')
  await raw.trigger('click')
  await flushPromises()
  expect(engine.setFormatExcluded).toHaveBeenCalledExactlyOnceWith(Family.FamilyRAW, true)
  expect(lib.settings.excludedFormats).toEqual([Family.FamilyRAW])
  expect(wrapper.text()).toContain(t('settings.formatsOff', { n: 1 }))

  vi.mocked(engine.setFormatExcluded).mockRejectedValueOnce(new Error('Disk full'))
  vi.mocked(engine.settings).mockResolvedValueOnce({ ...settings, excludedFormats: [Family.FamilyRAW] })
  await wrapper.get('[data-format="heif"]').trigger('click')
  await flushPromises()
  expect(lib.settings.excludedFormats).toEqual([Family.FamilyRAW])
  expect(wrapper.get('[role="alert"]').text()).toContain('Disk full')

  await wrapper.get('[data-format="raw"]').trigger('click')
  await flushPromises()
  expect(engine.setFormatExcluded).toHaveBeenLastCalledWith(Family.FamilyRAW, false)
  expect(lib.settings.excludedFormats).toEqual([])
})

it('saves a named export preset, refuses a nameless or repeated one, and removes one', async () => {
  const prefs = usePreferences()
  const wrapper = show(ExportPresets)
  await wrapper.get('[data-preset-new]').trigger('click')
  await flushPromises()
  const dialog = new DOMWrapper(document.body)
  expect(wrapper.text()).toContain(t('settings.presetNone'))
  await dialog.get('form').trigger('submit')
  await flushPromises()
  expect(dialog.get('[role="alert"]').text()).toContain('Give the preset a name')
  expect(prefs.prefs.exportPresets).toEqual([])

  await dialog.get('[data-preset-name]').setValue('  Web  ')
  await dialog.get('form').trigger('submit')
  await flushPromises()
  expect(prefs.prefs.exportPresets).toEqual([
    { name: 'Web', format: ExportFormat.ExportJPEG, side: 4096, quality: 92, adjust: true },
  ])
  expect(wrapper.get('[data-preset="Web"]').text()).toContain('quality 92')

  await wrapper.get('[data-preset-new]').trigger('click')
  await flushPromises()
  await dialog.get('[data-preset-name]').setValue('web')
  await dialog.get('form').trigger('submit')
  await flushPromises()
  expect(dialog.get('[role="alert"]').text()).toBe(t('settings.presetNameTaken'))
  expect(prefs.prefs.exportPresets).toHaveLength(1)

  const cancel = dialog.findAll('button').find((button) => button.text() === t('common.cancel'))
  await cancel?.trigger('click')
  const notices = recordNotifications()
  await wrapper.get('[data-preset="Web"] button').trigger('click')
  await flushPromises()
  expect(prefs.prefs.exportPresets).toEqual([])
  notices.find('settings.presetRemoved')?.actions?.[0]?.run()
  await flushPromises()
  expect(prefs.prefs.exportPresets[0]?.name).toBe('Web')
  notices.stop()
})

it('explains a preset name the engine folds to an existing name and keeps it available to edit', async () => {
  const prefs = usePreferences()
  const existing = { name: 'S', format: ExportFormat.ExportJPEG, side: 4096, quality: 92, adjust: true }
  await prefs.setExportPresets([existing])
  vi.mocked(engine.setPreferences).mockImplementationOnce((p) => Promise.resolve({ ...p, exportPresets: [existing] }))
  const wrapper = show(ExportPresets)
  await wrapper.get('[data-preset-new]').trigger('click')
  await flushPromises()
  const dialog = new DOMWrapper(document.body)
  await dialog.get('[data-preset-name]').setValue('ſ')
  await dialog.get('form').trigger('submit')
  await flushPromises()
  expect(dialog.get('[role="alert"]').text()).toBe(t('settings.presetNameTaken'))
  expect(dialog.get<HTMLInputElement>('[data-preset-name]').element.value).toBe('ſ')
  expect(prefs.prefs.exportPresets).toEqual([existing])
})
