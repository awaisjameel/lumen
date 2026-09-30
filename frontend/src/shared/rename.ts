import { nextTick, ref, shallowRef } from 'vue'
import type { ComponentPublicInstance } from 'vue'

type ElementRef = Element | ComponentPublicInstance | null

/**
 * Keyboard behaviour shared by the inline name editors (an event's, a
 * person's): opening one focuses its field with the name selected, and
 * closing it (saved, cancelled with Escape) puts focus back on the control
 * that opened it, which is drawn again in the field's place. key says which
 * item is being renamed, for lists; a single editor uses one key.
 */
export function useRename<K>() {
  const editing = shallowRef<K | null>(null)
  const draft = ref('')
  let field: HTMLInputElement | null = null
  const triggers = new Map<K, HTMLElement>()

  /** A :ref for the name field. */
  function fieldRef(el: ElementRef) {
    field = el instanceof HTMLInputElement ? el : null
  }

  /** A :ref for the control that opens key's editor. */
  function triggerRef(key: K) {
    return (el: ElementRef) => {
      if (el instanceof HTMLElement) triggers.set(key, el)
      else triggers.delete(key)
    }
  }

  async function open(key: K, name: string) {
    editing.value = key
    draft.value = name
    await nextTick()
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
