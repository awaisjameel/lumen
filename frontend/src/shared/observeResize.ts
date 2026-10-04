/**
 * Measures at mount and once per frame after a resize. Updating a virtual
 * layout during observer delivery can change the observed element again
 * in that same delivery, leaving notifications undelivered. Disposal also
 * cancels the pending frame so a closed view cannot update its layout.
 */
export function observeResize(elements: readonly Element[], measure: () => void): () => void {
  let frame: number | undefined
  const observer = new ResizeObserver(() => {
    if (frame !== undefined) return
    frame = requestAnimationFrame(() => {
      frame = undefined
      measure()
    })
  })
  measure()
  for (const element of elements) observer.observe(element)
  return () => {
    observer.disconnect()
    if (frame !== undefined) cancelAnimationFrame(frame)
  }
}
