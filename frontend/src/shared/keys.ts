/**
 * Whether a key event comes from a text field, where letters and arrows
 * belong to the typing, not to single-key shortcuts (F, I, /, ←…).
 */
export function isTyping(e: KeyboardEvent): boolean {
  const el = e.target
  return (
    el instanceof HTMLTextAreaElement ||
    (el instanceof HTMLElement && el.isContentEditable) ||
    (el instanceof HTMLInputElement && !['checkbox', 'radio', 'range', 'button'].includes(el.type))
  )
}
