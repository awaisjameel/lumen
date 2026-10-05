import { nextTick, ref, shallowRef } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { whenPortalsClosed } from './overlay'

type ElementRef = Element | ComponentPublicInstance | null

interface Focusable {
  focus: () => void
}

/** What a :ref gave, as something to focus: an element, a component that exposes its focus, or one drawn as a single element. */
function focusable(el: unknown): Focusable | null {
  if (el instanceof HTMLElement) return el
  if (!el || typeof el !== 'object') return null
  if ('focus' in el && typeof el.focus === 'function') return el as Focusable
  return '$el' in el && el.$el instanceof HTMLElement ? el.$el : null
}

/**
 * Keyboard behaviour shared by the inline name editors (an event's, a
 * person's): opening one focuses its field with the name selected, and
 * closing it (saved, cancelled with Escape) puts focus back on the control
 * that opened it, which is drawn again in the field's place. key says which
 * item is being renamed, for lists; a single editor uses one key. Opened
 * from a menu or the command palette, the field takes the focus once that
 * overlay has closed: while open it holds the focus itself.
 */
export function useRename<K>() {
  const editing = shallowRef<K | null>(null)
  const draft = ref('')
  let field: HTMLInputElement | null = null
  const triggers = new Map<K, Focusable>()

  /** A :ref for the name field. */
  function fieldRef(el: ElementRef) {
    field = el instanceof HTMLInputElement ? el : null
  }

  /** A :ref for the control that opens key's editor. */
  function triggerRef(key: K) {
    return (el: unknown) => {
      const trigger = focusable(el)
      if (trigger) triggers.set(key, trigger)
      else triggers.delete(key)
    }
  }

  async function open(key: K, name: string) {
    editing.value = key
    draft.value = name
    await nextTick()
    await whenPortalsClosed()
    if (editing.value !== key) return // closed, or another opened, meanwhile
    field?.focus()
    field?.select()
  }

  /** Closes the editor; with refocus, focus returns to what opened it. */
  async function close(refocus = true) {
    const key = editing.value
    editing.value = null
    if (!refocus || key === null) return
    await nextTick()
    triggers.get(key)?.focus()
  }

  return { editing, draft, fieldRef, triggerRef, open, close }
}
