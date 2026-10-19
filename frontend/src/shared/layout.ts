// The shell's three layouts, from the window's width (design tokens name the
// same breakpoints for container queries). Wide: a resizable sidebar and an
// inline inspector. Medium: the sidebar is a rail of icons and the
// inspector floats over the content as a sheet. Narrow (below Lumen's
// minimum window, so only when the window manager forces it): navigation
// opens as a drawer.
import { onScopeDispose, readonly, ref } from 'vue'
import type { Ref } from 'vue'

const WIDE_PX = 1024
const MEDIUM_PX = 720

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
