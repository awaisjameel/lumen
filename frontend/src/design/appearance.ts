// How the interface preferences become what the page looks like. The
// preferences store (shared/preferences.ts) is the one owner of the values;
// this file only applies them: the classes and attributes the tokens read
// (tokens.css), and the two things Nuxt UI takes from its own config (the
// accent colour and its controls' size). Portalled overlays follow, because
// everything is set on the document's root.
import { useAppConfig } from '@nuxt/ui/runtime/vue/composables/useAppConfig.js'
import { Accent, Density, Motion, TEXT_SCALE_MAX, TEXT_SCALE_MIN, Theme } from '../bridge'
import type { Preferences } from '../bridge'

/** The preferences that decide the first frame. */
export type Appearance = Pick<Preferences, 'theme' | 'accent' | 'density' | 'textScale'>

/**
 * The preferences that change how the page is drawn once it runs: how much
 * it moves, what surrounds a photo in the viewer, how focus is drawn, how
 * thumbnails fill their tiles and which badges they carry. Each becomes an
 * attribute on the root that tokens.css and MediaTile read.
 */
export type Presentation = Pick<
  Preferences,
  | 'motion'
  | 'viewerBackground'
  | 'focusRing'
  | 'thumbFit'
  | 'hideFavouriteBadge'
  | 'hideVideoBadge'
  | 'hideDurationBadge'
>

// The last appearance applied, for painting the first frame before the
// engine has answered. It is derived from the library's settings and
// rewritten whenever they are applied: never read as the user's choice.
const CACHE_KEY = 'lumen.appearance'

// The generated enums also carry Go's zero value (""), which is no choice.
const themes: readonly string[] = Object.values(Theme).filter((v) => v !== Theme.$zero)
const accents: readonly string[] = Object.values(Accent).filter((v) => v !== Accent.$zero)
const densities: readonly string[] = Object.values(Density).filter((v) => v !== Density.$zero)

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null
}
function isTheme(v: unknown): v is Theme {
  return typeof v === 'string' && themes.includes(v)
}
function isAccent(v: unknown): v is Accent {
  return typeof v === 'string' && accents.includes(v)
}
function isDensity(v: unknown): v is Density {
  return typeof v === 'string' && densities.includes(v)
}

/** The cached appearance, or null if there is none that can be applied. */
export function cachedAppearance(): Appearance | null {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(CACHE_KEY) ?? 'null')
    if (
      isRecord(v) &&
      isTheme(v.theme) &&
      isAccent(v.accent) &&
      isDensity(v.density) &&
      typeof v.textScale === 'number' &&
      Number.isFinite(v.textScale)
    ) {
      return {
        theme: v.theme,
        accent: v.accent,
        density: v.density,
        textScale: Math.round(Math.min(TEXT_SCALE_MAX, Math.max(TEXT_SCALE_MIN, v.textScale))),
      }
    }
  } catch {
    /* storage unavailable or unreadable: paint the defaults */
  }
  return null
}

const systemDark = window.matchMedia('(prefers-color-scheme: dark)')
const systemReduced = window.matchMedia('(prefers-reduced-motion: reduce)')
let applied: Appearance | null = null
let presented: Presentation | null = null

// Nuxt UI's controls whose size follows the density.
const SIZED = [
  'button',
  'input',
  'inputNumber',
  'select',
  'selectMenu',
  'textarea',
  'checkbox',
  'radioGroup',
  'switch',
  'tabs',
] as const

/** Sets Nuxt UI's accent colour and control size, which it reads from its reactive config. */
function configure(accent: Accent, density: Density): void {
  // Nuxt UI types its Vue config as any; only these two parts are touched.
  const config: unknown = useAppConfig()
  if (!isRecord(config) || !isRecord(config.ui)) return
  const ui = config.ui
  if (isRecord(ui.colors)) ui.colors.primary = accent
  const size = density === Density.DensityCompact ? 'sm' : 'md'
  for (const name of SIZED) {
    const component = isRecord(ui[name]) ? ui[name] : {}
    const variants = isRecord(component.defaultVariants) ? component.defaultVariants : {}
    ui[name] = { ...component, defaultVariants: { ...variants, size } }
  }
}

function paint(a: Appearance): void {
  const root = document.documentElement
  const dark = a.theme === Theme.ThemeDark || (a.theme === Theme.ThemeSystem && systemDark.matches)
  root.classList.toggle('dark', dark)
  root.dataset.accent = a.accent
  root.dataset.density = a.density
  root.style.setProperty('--lumen-text-scale', String(a.textScale / 100))
}

function present(p: Presentation): void {
  const root = document.documentElement
  // "System" follows Windows' animation setting; the other two say so
  // whatever Windows has.
  const reduced = p.motion === Motion.MotionReduced || (p.motion === Motion.MotionSystem && systemReduced.matches)
  root.dataset.motion = reduced ? 'reduced' : 'full'
  root.dataset.viewerBg = p.viewerBackground
  root.dataset.focus = p.focusRing
  root.dataset.thumbFit = p.thumbFit
  root.toggleAttribute('data-hide-favourite', p.hideFavouriteBadge)
  root.toggleAttribute('data-hide-video', p.hideVideoBadge)
  root.toggleAttribute('data-hide-duration', p.hideDurationBadge)
}

/** Draws the page as these preferences say. Nothing of it is cached: it follows the first frame. */
export function applyPresentation(p: Presentation): void {
  presented = {
    motion: p.motion,
    viewerBackground: p.viewerBackground,
    focusRing: p.focusRing,
    thumbFit: p.thumbFit,
    hideFavouriteBadge: p.hideFavouriteBadge,
    hideVideoBadge: p.hideVideoBadge,
    hideDurationBadge: p.hideDurationBadge,
  }
  present(presented)
}

/** Whether the interface is holding still now: the user's choice, or Windows' under "system". */
export function motionReduced(): boolean {
  return document.documentElement.dataset.motion === 'reduced'
}

/** Shows the interface as a says, and remembers it for the next start's first frame. */
export function applyAppearance(a: Appearance): void {
  applied = { theme: a.theme, accent: a.accent, density: a.density, textScale: a.textScale }
  paint(applied)
  configure(a.accent, a.density)
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify(applied))
  } catch {
    /* not critical: the next start paints the defaults first */
  }
}

// "System" follows Windows while Lumen runs.
systemDark.addEventListener('change', () => {
  if (applied) paint(applied)
})
systemReduced.addEventListener('change', () => {
  if (presented) present(presented)
})
