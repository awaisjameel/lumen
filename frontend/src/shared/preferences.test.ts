import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { Accent, Density, engine, Theme } from '../bridge'
import type * as Bridge from '../bridge'
import type { Preferences } from '../bridge'
import { ERROR_NOTIFICATION } from './errors'
import { usePreferences } from './preferences'
import { recordNotifications } from './testing'
import type { RecordedNotifications } from './testing'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    migratePreferences: vi.fn<typeof Bridge.engine.migratePreferences>(),
    setPreferences: vi.fn<typeof Bridge.engine.setPreferences>(),
  },
  logUIError: vi.fn<typeof Bridge.logUIError>(),
}))

const stored: Preferences = {
  theme: Theme.ThemeDark,
  accent: Accent.AccentTeal,
  density: Density.DensityCompact,
  textScale: 110,
  tileSize: 200,
  sidebarCollapsed: false,
  sidebarWidth: 260,
  inspectorWidth: 340,
}

let notified: RecordedNotifications

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
  document.documentElement.className = ''
  notified = recordNotifications()
  vi.mocked(engine.migratePreferences).mockReset().mockResolvedValue(stored)
  vi.mocked(engine.setPreferences)
    .mockReset()
    .mockImplementation((p) => Promise.resolve(p))
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
})
afterEach(() => notified.stop())

const root = () => document.documentElement

it('paints the defaults first, then what the library holds', async () => {
  const prefs = usePreferences()
  expect(prefs.loaded).toBe(false)
  expect(root().classList.contains('dark')).toBe(false) // the test system is light
  expect(root().dataset.accent).toBe('blue')

  await prefs.load()
  expect(prefs.loaded).toBe(true)
  expect(prefs.prefs).toEqual(stored)
  expect(root().classList.contains('dark')).toBe(true)
  expect(root().dataset.accent).toBe('teal')
  expect(root().dataset.density).toBe('compact')
  expect(root().style.getPropertyValue('--lumen-text-scale')).toBe('1.1')
})

it('offers the library what an older Lumen kept in the WebView, without writing it again', async () => {
  localStorage.setItem('lumen.theme', 'dark')
  localStorage.setItem('lumen.zoom', '240')
  await usePreferences().load()
  expect(engine.migratePreferences).toHaveBeenCalledExactlyOnceWith({ theme: Theme.ThemeDark, tileSize: 240 })
  // The legacy values are only read: the library's settings are the owner now.
  expect(localStorage.getItem('lumen.theme')).toBe('dark')
  expect(localStorage.getItem('lumen.zoom')).toBe('240')
})

it('shows an older Lumen’s theme and thumbnail size from the first frame after upgrading', () => {
  localStorage.setItem('lumen.theme', 'dark')
  localStorage.setItem('lumen.zoom', '240')
  const prefs = usePreferences()
  expect(prefs.loaded).toBe(false)
  expect(prefs.prefs.theme).toBe(Theme.ThemeDark)
  expect(prefs.prefs.tileSize).toBe(240)
  expect(root().classList.contains('dark')).toBe(true)
})

it('offers nothing when the WebView holds no legacy choice, or one it cannot read', async () => {
  localStorage.setItem('lumen.theme', 'sepia')
  localStorage.setItem('lumen.zoom', 'big')
  await usePreferences().load()
  expect(engine.migratePreferences).toHaveBeenCalledExactlyOnceWith({ theme: Theme.$zero, tileSize: 0 })
})

it('starts the next session from the appearance last applied, not from a stale choice', async () => {
  const first = usePreferences()
  await first.load()
  // A new session: the first frame uses the cache, before the engine answers.
  setActivePinia(createPinia())
  document.documentElement.className = ''
  const next = usePreferences()
  expect(next.loaded).toBe(false)
  expect(next.prefs.theme).toBe(Theme.ThemeDark)
  expect(root().classList.contains('dark')).toBe(true)
  // The cache holds only the appearance: sizes come from the library.
  expect(next.prefs.tileSize).not.toBe(stored.tileSize)
})

it('shows a change at once and saves all the preferences together', async () => {
  const prefs = usePreferences()
  await prefs.load()
  const saved = prefs.set({ theme: Theme.ThemeLight })
  expect(prefs.prefs.theme).toBe(Theme.ThemeLight)
  expect(root().classList.contains('dark')).toBe(false)
  expect(prefs.saving).toBe(true)
  await saved
  expect(prefs.saving).toBe(false)
  expect(engine.setPreferences).toHaveBeenCalledExactlyOnceWith({ ...stored, theme: Theme.ThemeLight })
})

it('saves rapid changes one at a time and ends with the newest', async () => {
  const prefs = usePreferences()
  await prefs.load()
  let release: (() => void) | undefined
  vi.mocked(engine.setPreferences).mockImplementationOnce((p) => new Promise((resolve) => (release = () => resolve(p))))
  const all = [prefs.set({ tileSize: 208 }), prefs.set({ tileSize: 216 }), prefs.set({ tileSize: 224 })]
  await flushPromises()
  expect(engine.setPreferences).toHaveBeenCalledTimes(1) // the first is still saving
  release?.()
  await Promise.all(all)
  // The middle value was superseded before its turn came.
  expect(vi.mocked(engine.setPreferences).mock.calls.map(([p]) => p.tileSize)).toEqual([208, 224])
  expect(prefs.prefs.tileSize).toBe(224)
  expect(prefs.saving).toBe(false)
})

it('rolls back to what the library holds when a save fails, and says why', async () => {
  const prefs = usePreferences()
  await prefs.load()
  vi.mocked(engine.setPreferences).mockRejectedValueOnce(new Error('disk full'))
  await prefs.set({ accent: Accent.AccentRose, theme: Theme.ThemeLight })
  expect(prefs.prefs).toEqual(stored)
  expect(root().dataset.accent).toBe('teal')
  expect(root().classList.contains('dark')).toBe(true)
  expect(notified.find(ERROR_NOTIFICATION)?.title).toContain('disk full')
})

it('keeps a newer successful change when an older save fails after it', async () => {
  const prefs = usePreferences()
  await prefs.load()
  let fail: (() => void) | undefined
  vi.mocked(engine.setPreferences).mockImplementationOnce(
    () => new Promise((_resolve, reject) => (fail = () => reject(new Error('busy')))),
  )
  const first = prefs.set({ tileSize: 120 })
  const second = prefs.set({ tileSize: 300 })
  await flushPromises()
  fail?.()
  await Promise.all([first, second])
  expect(prefs.prefs.tileSize).toBe(300)
  expect(notified.shown).toEqual([])
})

it('waits for the library before saving startup edits and preserves its other choices', async () => {
  let release: ((p: Preferences) => void) | undefined
  vi.mocked(engine.migratePreferences).mockReturnValueOnce(new Promise((resolve) => (release = resolve)))
  const prefs = usePreferences()
  const loading = prefs.load()
  const first = prefs.set({ theme: Theme.ThemeLight })
  const second = prefs.set({ tileSize: 240 })
  await flushPromises()
  expect(engine.migratePreferences).toHaveBeenCalledOnce()
  expect(engine.setPreferences).not.toHaveBeenCalled()
  release?.(stored)
  await Promise.all([loading, first, second])
  expect(prefs.prefs).toEqual({ ...stored, theme: Theme.ThemeLight, tileSize: 240 })
  expect(engine.setPreferences).toHaveBeenLastCalledWith(prefs.prefs)
})

it('does not reload preferences over a newer local edit', async () => {
  const prefs = usePreferences()
  await prefs.load()
  await prefs.set({ accent: Accent.AccentRose })
  await prefs.load()
  expect(prefs.prefs.accent).toBe(Accent.AccentRose)
  expect(engine.migratePreferences).toHaveBeenCalledOnce()
})

it('keeps startup edits away from storage when loading fails, and retries on the next edit', async () => {
  vi.mocked(engine.migratePreferences).mockRejectedValueOnce(new Error('library unavailable'))
  const prefs = usePreferences()
  await prefs.set({ theme: Theme.ThemeLight })
  expect(engine.setPreferences).not.toHaveBeenCalled()
  expect(prefs.loaded).toBe(false)
  expect(prefs.saving).toBe(false)
  expect(notified.find(ERROR_NOTIFICATION)?.title).toContain('library unavailable')
  await prefs.set({ theme: Theme.ThemeLight })
  expect(engine.setPreferences).toHaveBeenCalledExactlyOnceWith({ ...stored, theme: Theme.ThemeLight })
})

it('shows the engine’s normalised choices and rolls back to them after a later failure', async () => {
  const prefs = usePreferences()
  await prefs.load()
  const normalised = { ...stored, tileSize: 320 }
  vi.mocked(engine.setPreferences).mockResolvedValueOnce(normalised)
  await prefs.set({ tileSize: 999 })
  expect(prefs.prefs).toEqual(normalised)
  vi.mocked(engine.setPreferences).mockRejectedValueOnce(new Error('disk full'))
  await prefs.set({ theme: Theme.ThemeLight })
  expect(prefs.prefs).toEqual(normalised)
})

it('holds a stale first-paint text scale to the supported range before the engine answers', () => {
  localStorage.setItem('lumen.appearance', JSON.stringify({ ...stored, textScale: -100 }))
  const prefs = usePreferences()
  expect(prefs.prefs.textScale).toBe(90)
  expect(root().style.getPropertyValue('--lumen-text-scale')).toBe('0.9')
})
