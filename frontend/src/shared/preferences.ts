import { defineStore } from 'pinia'
import { ref, shallowRef } from 'vue'
import {
  Accent,
  Density,
  engine,
  INSPECTOR_WIDTH_DEFAULT,
  SIDEBAR_WIDTH_DEFAULT,
  TEXT_SCALE_DEFAULT,
  Theme,
  TILE_SIZE_DEFAULT,
  TILE_SIZE_MAX,
  TILE_SIZE_MIN,
} from '../bridge'
import type { LegacyPreferences, Preferences } from '../bridge'
import { applyAppearance, cachedAppearance } from '../design/appearance'
import { reportError } from './errors'
import { latestSave } from './latestSave'
import type { LatestSave } from './latestSave'

/** What a library starts with (core.DefaultPreferences). */
function defaults(): Preferences {
  return {
    theme: Theme.ThemeSystem,
    accent: Accent.AccentBlue,
    density: Density.DensityComfortable,
    textScale: TEXT_SCALE_DEFAULT,
    tileSize: TILE_SIZE_DEFAULT,
    sidebarCollapsed: false,
    sidebarWidth: SIDEBAR_WIDTH_DEFAULT,
    inspectorWidth: INSPECTOR_WIDTH_DEFAULT,
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
  const prefs = shallowRef<Preferences>({ ...defaults(), ...legacyChoices(), ...cachedAppearance() })
  const loaded = ref(false)
  const saving = ref(false)
  applyAppearance(prefs.value)

  function show(p: Preferences) {
    prefs.value = p
    applyAppearance(p)
  }

  function saver(initial: Preferences): LatestSave<Preferences> {
    return latestSave<Preferences>((p) => engine.setPreferences(p), initial, {
      // A failed save shows the last values the library holds again.
      rollback: show,
      failed: reportError,
    })
  }
  let save = saver(prefs.value)
  let pending = 0

  /** Reads the library's preferences, first offering it an older Lumen's choices. */
  async function load(): Promise<void> {
    const p = await engine.migratePreferences(legacy())
    save = saver(p)
    show(p)
    loaded.value = true
  }

  /**
   * Changes preferences: shown at once, then saved. Rapid changes (a slider)
   * save one at a time and end with the newest; a failure rolls back to
   * what the library holds and says why.
   */
  async function set(patch: Partial<Preferences>): Promise<void> {
    show({ ...prefs.value, ...patch })
    pending++
    saving.value = true
    try {
      await save(prefs.value)
    } finally {
      if (--pending === 0) saving.value = false
    }
  }

  return { prefs, loaded, saving, load, set }
})
