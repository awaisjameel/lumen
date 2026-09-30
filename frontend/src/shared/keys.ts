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

/**
 * Whether a key event comes from a control that uses the arrow keys itself:
 * a slider, or a video's own controls.
 */
export function ownsArrows(e: KeyboardEvent): boolean {
  const el = e.target
  return isTyping(e) || (el instanceof HTMLInputElement && el.type === 'range') || el instanceof HTMLMediaElement
}

/** Whether a modal (the viewer, a confirmation) covers the page, so page shortcuts must wait. */
export function modalOpen(): boolean {
  return document.querySelector('[aria-modal="true"], dialog[open]') !== null
}
