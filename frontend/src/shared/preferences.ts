import { defineStore } from 'pinia'
import { computed, ref, shallowRef } from 'vue'
import {
  Accent,
  Density,
  engine,
  FocusRing,
  INSPECTOR_WIDTH_DEFAULT,
  Motion,
  SIDEBAR_WIDTH_DEFAULT,
  StartView,
  TEXT_SCALE_DEFAULT,
  Theme,
  ThumbFit,
  TILE_SIZE_DEFAULT,
  TILE_SIZE_MAX,
  TILE_SIZE_MIN,
  ViewerBackground,
} from '../bridge'
import type { ExportPreset, LegacyPreferences, PrefCategory, Preferences } from '../bridge'
import { applyAppearance, applyPresentation, cachedAppearance } from '../design/appearance'
import { setShortcutOverrides } from './commands'
import { reportError } from './errors'
import { latestSave } from './latestSave'
import type { LatestSave } from './latestSave'
import { viewerDefaults } from './viewing'
import type { ViewerPreferences } from '../bridge'

/** What a library starts with (core.DefaultPreferences). */
export function defaultPreferences(): Preferences {
  return {
    viewer: viewerDefaults(),
    theme: Theme.ThemeSystem,
    accent: Accent.AccentBlue,
    density: Density.DensityComfortable,
    textScale: TEXT_SCALE_DEFAULT,
    tileSize: TILE_SIZE_DEFAULT,
    sidebarCollapsed: false,
    sidebarWidth: SIDEBAR_WIDTH_DEFAULT,
    inspectorWidth: INSPECTOR_WIDTH_DEFAULT,
    motion: Motion.MotionSystem,
    viewerBackground: ViewerBackground.ViewerBackgroundDark,
    startView: StartView.StartLibrary,
    thumbFit: ThumbFit.ThumbCover,
    focusRing: FocusRing.FocusStandard,
    hideFavouriteBadge: false,
    hideVideoBadge: false,
    hideDurationBadge: false,
    shortcuts: {},
    exportPresets: [],
  }
}

/**
 * What an older Lumen kept in the WebView's storage: the theme and the grid's
 * thumbnail size. They are only read, to offer them to the library once
 * (the engine decides whether they apply); nothing writes them any more.
 */
function legacy(): LegacyPreferences {
  const out: LegacyPreferences = { theme: Theme.$zero, tileSize: 0 }
  try {
    const theme = localStorage.getItem('lumen.theme')
    if (theme === Theme.ThemeLight || theme === Theme.ThemeDark) out.theme = theme
    const zoom = Number(localStorage.getItem('lumen.zoom'))
    if (Number.isInteger(zoom) && zoom > 0) out.tileSize = zoom
  } catch {
    /* storage unavailable: there is nothing to migrate */
  }
  return out
}

/** The legacy choices as preferences, for the frames before the engine has answered. */
function legacyChoices(): Partial<Preferences> {
  const old = legacy()
  return {
    ...(old.theme === Theme.$zero ? {} : { theme: old.theme }),
    ...(old.tileSize >= TILE_SIZE_MIN && old.tileSize <= TILE_SIZE_MAX ? { tileSize: old.tileSize } : {}),
  }
}

/**
 * The interface preferences: theme, accent, density, text size, the grid's
 * thumbnail size and the panels' widths. The library's settings own them
 * (they follow the library, not the WebView); this store holds the session's
 * copy, applies it, and saves each change in order.
 */
export const usePreferences = defineStore('preferences', () => {
  // Until the engine answers, the first frame uses the last appearance
  // applied on this PC over the defaults, so the theme does not flash; on
  // the first start after an older Lumen, what that one had chosen.
  const prefs = shallowRef<Preferences>({ ...defaultPreferences(), ...legacyChoices(), ...cachedAppearance() })
  const loaded = ref(false)
  const saving = ref(false)
  apply(prefs.value)

  /** Makes the page, and the shortcuts, what p says. */
  function apply(p: Preferences) {
    applyAppearance(p)
    applyPresentation(p)
    setShortcutOverrides(p.shortcuts)
  }

  function show(p: Preferences) {
    prefs.value = p
    apply(p)
  }

  let saveFailed = false
  function saver(initial: Preferences): LatestSave<Preferences> {
    let stored = initial
    return latestSave<Preferences>(
      async (p) => {
        stored = await engine.setPreferences(p)
      },
      initial,
      {
        saved: () => {
          saveFailed = false
          show(stored)
        },
        // A failed save shows the last values the library holds again.
        rollback: () => show(stored),
        failed: (e) => {
          saveFailed = true
          reportError(e)
        },
      },
    )
  }
  let save = saver(prefs.value)
  let pending = 0
  let loading: Promise<void> | null = null
  let resetting: Promise<void> | null = null

  /** Reads the library's preferences, first offering it an older Lumen's choices. */
  function load(): Promise<void> {
    if (loaded.value) return Promise.resolve()
    loading ??= (async () => {
      const p = await engine.migratePreferences(legacy())
      save = saver(p)
      show(p)
      loaded.value = true
    })().finally(() => {
      loading = null
    })
    return loading
  }

  /**
   * Changes preferences: shown at once, then saved. Rapid changes (a slider)
   * save one at a time and end with the newest; a failure rolls back to
   * what the library holds and says why.
   */
  async function set(patch: Partial<Preferences>): Promise<boolean> {
    pending++
    saving.value = true
    try {
      // A startup edit must preserve the library's other choices rather
      // than saving the first-paint cache as a whole preference set.
      if (!loaded.value) await load()
      while (resetting) await resetting
      show({ ...prefs.value, ...patch })
      await save(prefs.value)
      return !saveFailed
    } catch (e) {
      reportError(e)
      return false
    } finally {
      if (--pending === 0) saving.value = false
    }
  }

  /** The viewing and playback choices; the fallback holds only until the library answers. */
  const viewer = computed(() => prefs.value.viewer ?? viewerDefaults())

  async function setViewer(patch: Partial<ViewerPreferences>): Promise<void> {
    try {
      if (!loaded.value) await load()
      while (resetting) await resetting
      await set({ viewer: { ...viewer.value, ...patch } })
    } catch (e) {
      reportError(e)
    }
  }

  /** Reassigns a command's shortcut ("" leaves it without one), or gives it back its own (undefined). */
  async function setShortcut(id: string, keys: string | undefined): Promise<boolean> {
    try {
      if (!loaded.value) await load()
      while (resetting) await resetting
      const others = Object.entries(prefs.value.shortcuts).filter(([command]) => command !== id)
      const shortcuts = Object.fromEntries(keys === undefined ? others : [...others, [id, keys]])
      return await set({ shortcuts })
    } catch (e) {
      reportError(e)
      return false
    }
  }

  /** Replaces the saved export presets. */
  function setExportPresets(exportPresets: ExportPreset[]): Promise<boolean> {
    return set({ exportPresets })
  }

  /**
   * Puts whole categories of settings back to their defaults in the library,
   * then shows the preferences as it now holds them. Changes still on their
   * way are saved first, so the reset is the last word. It throws what the
   * engine refused, for the caller to explain beside its button.
   */
  function reset(categories: PrefCategory[]): Promise<void> {
    const previous = resetting
    const task = (async () => {
      if (previous) await previous
      if (!loaded.value) await load()
      await save.flush()
      try {
        await engine.resetPreferences(categories)
      } finally {
        const p = await engine.preferences()
        save = saver(p)
        show(p)
      }
    })()
    // Later edits wait for completion; the caller of reset receives its failure.
    const current = task
      .catch(() => undefined)
      .finally(() => {
        if (resetting === current) resetting = null
      })
    resetting = current
    return task
  }

  return { prefs, viewer, loaded, saving, load, set, setViewer, setShortcut, setExportPresets, reset }
})
