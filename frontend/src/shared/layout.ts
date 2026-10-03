// The shell's three layouts, from the window's width (design tokens name the
// same breakpoints for container queries). Wide: a resizable sidebar and an
// inline inspector. Medium: navigation collapsed to icons, the inspector
// optional. Narrow: a navigation drawer, overflow actions and the inspector
// as a sheet.
import { onScopeDispose, readonly, ref } from 'vue'
import type { Ref } from 'vue'

export const WIDE_PX = 1200
export const MEDIUM_PX = 900

export type LayoutMode = 'wide' | 'medium' | 'narrow'

function modeOf(width: number): LayoutMode {
  return width >= WIDE_PX ? 'wide' : width >= MEDIUM_PX ? 'medium' : 'narrow'
}

/** The window's layout mode, updated as it is resized. */
export function useLayoutMode(): Readonly<Ref<LayoutMode>> {
  const mode = ref<LayoutMode>(modeOf(window.innerWidth))
  const update = () => (mode.value = modeOf(window.innerWidth))
  window.addEventListener('resize', update, { passive: true })
  onScopeDispose(() => window.removeEventListener('resize', update))
  return readonly(mode)
}
