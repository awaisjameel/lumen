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
 * Give portalled overlays their opening order, independently of where Vue
 * placed their persistent portal containers. A global confirmation can be
 * earlier in the DOM than the feature dialog it must cover. Reka still owns
 * dismissal and focus; this owner only supplies the matching paint order.
 */
export function startPortalLayers(): () => void {
  const active = new Map<HTMLElement, number>()
  const previous = new Map<HTMLElement, string>()
  const configured = Number.parseInt(getComputedStyle(document.documentElement).getPropertyValue('--lumen-z-portal'))
  let next = Number.isFinite(configured) ? configured : 40
  function paint(element: HTMLElement, z: number) {
    if (!previous.has(element)) previous.set(element, element.style.zIndex)
    element.style.zIndex = String(z)
  }
  function refresh() {
    const current = [...document.querySelectorAll(PORTALLED)].filter(
      (element): element is HTMLElement => element instanceof HTMLElement,
    )
    for (const element of active.keys()) if (!current.includes(element)) active.delete(element)
    for (const [element, value] of previous) {
      if (!element.isConnected) {
        element.style.zIndex = value
        previous.delete(element)
      }
    }
    for (const element of current) {
      let z = active.get(element)
      if (z === undefined) {
        z = next
        next += 2
        active.set(element, z)
      }
      paint(element, z)
      const popper = element.closest('[data-reka-popper-content-wrapper]')
      if (popper instanceof HTMLElement) paint(popper, z)
      const overlay = element.closest('[data-slot="overlay"]') ?? element.previousElementSibling
      if (overlay instanceof HTMLElement && overlay.dataset.slot === 'overlay') paint(overlay, z)
    }
    for (const tooltip of document.querySelectorAll('[role="tooltip"]')) {
      const popper = tooltip.closest('[data-reka-popper-content-wrapper]')
      if (popper instanceof HTMLElement) paint(popper, next)
    }
  }
  const observer = new MutationObserver((records) => {
    for (const record of records) if (record.target === document.body) record.addedNodes.forEach(watch)
    refresh()
  })
  // Overlays are teleported to the body, beside the root the application is
  // mounted in. Only they are watched: the page inside that root changes with
  // every scroll of the grid, and holds no overlay.
  function watch(node: Node) {
    if (!(node instanceof HTMLElement) || node.hasAttribute('data-v-app')) return
    observer.observe(node, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['data-state', 'role', 'open'],
    })
  }
  observer.observe(document.body, { childList: true })
  Array.from(document.body.children).forEach(watch)
  refresh()
  return () => {
    observer.disconnect()
    for (const [element, value] of previous) element.style.zIndex = value
  }
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
