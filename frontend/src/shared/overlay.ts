// The overlay coordinator: which layer is on top, who owns Escape, and when
// page shortcuts must wait. Lumen's own full-window layers (the viewer, the
// face review) register here; Nuxt UI's portalled overlays (dialogs, menus,
// popovers, selects, the command palette) are recognised in the document,
// where Reka UI marks each open one. Focus trapping and restoration are
// Reka UI's for its overlays and the layer's own for Lumen's.
import { onBeforeUnmount, shallowRef, toValue, watch } from 'vue'
import type { MaybeRefOrGetter } from 'vue'

const layers = shallowRef<readonly symbol[]>([])

// What Reka UI renders while one of its overlays that takes the keyboard is
// open (a dialog, sheet or popover; a menu; a select's list), plus native
// modal dialogs. A tooltip is not one: it appears on hover and takes no key.
const PORTALLED = [
  '[role="dialog"][data-state="open"]',
  '[role="alertdialog"][data-state="open"]',
  '[role="menu"][data-state="open"]',
  '[role="listbox"][data-state="open"]',
  'dialog[open]',
].join(', ')

/** Whether a dialog, menu, popover or select of Nuxt UI's is open. */
export function portalOpen(): boolean {
  return document.querySelector(PORTALLED) !== null
}

/**
 * Resolves once no portalled overlay is open: at once if none is, else when
 * the open one has gone (a menu closing after its item was chosen), or after
 * wait ms at the latest. An open overlay traps the focus, so anything that
 * wants to take it (an inline editor a menu item opened) waits for this.
 */
export function whenPortalsClosed(wait = 600): Promise<void> {
  return new Promise((resolve) => {
    const end = performance.now() + wait
    const check = () => {
      if (!portalOpen() || performance.now() >= end) resolve()
      else requestAnimationFrame(check)
    }
    check()
  })
}

/** Whether any of Lumen's own layers is open. */
export function layerOpen(): boolean {
  return layers.value.length > 0
}

/**
 * Whether page shortcuts (the grid's keys, single-letter commands) must
 * wait: an overlay covers the page, so keys belong to it.
 */
export function shortcutsSuspended(): boolean {
  return layerOpen() || portalOpen()
}

/**
 * Registers a full-window layer of Lumen's while open is true, for as long
 * as the calling component is mounted. owns() says whether this layer may
 * act on a key (Escape, arrows): it is the top layer and no portalled
 * overlay is open above it.
 */
export function useLayer(open: MaybeRefOrGetter<boolean> = true): { owns: () => boolean } {
  const id = Symbol('layer')
  const remove = () => {
    if (layers.value.includes(id)) layers.value = layers.value.filter((l) => l !== id)
  }
  watch(
    () => toValue(open),
    (isOpen) => {
      if (isOpen && !layers.value.includes(id)) layers.value = [...layers.value, id]
      else if (!isOpen) remove()
    },
    { immediate: true },
  )
  onBeforeUnmount(remove)
  return { owns: () => layers.value.at(-1) === id && !portalOpen() }
}
