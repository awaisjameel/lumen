/** What latestSave does around each save. */
export interface LatestSaveHandlers<T> {
  /** After a save that is still the newest value asked for. */
  saved?: (value: T) => unknown
  /** A newest save failed: show value, the last one saved, again. */
  rollback: (value: T) => void
  /** Why the newest save failed. */
  failed: (e: unknown) => void
}

export interface LatestSave<T> {
  (value: T): Promise<void>
  /** Supersede older saves as soon as an input arrives, before its debounce. */
  touch: () => void
  /** Waits for current and queued saves without writing another value. */
  flush: () => Promise<void>
}

/**
 * Saves a setting that can change faster than the engine saves it (a number
 * typed in, a slider): one save at a time, then the newest value asked for
 * meanwhile, so the engine always ends with the last one. Only the newest
 * request may roll back or report a failure: an older one failing after a
 * newer one succeeded changes nothing on screen. The returned function
 * resolves once every queued save has run.
 */
export function latestSave<T>(
  save: (value: T) => Promise<unknown>,
  initial: NoInfer<T>,
  handlers: LatestSaveHandlers<T>,
): LatestSave<T> {
  let lastSaved = initial
  let queued: { value: T; revision: number } | null = null
  let revision = 0
  let running: Promise<void> | null = null
  const set = (value: T): Promise<void> => {
    queued = { value, revision: ++revision }
    running ??= (async () => {
      try {
        while (queued) {
          const { value: next, revision: requested } = queued
          queued = null
          try {
            await save(next)
            lastSaved = next
          } catch (e) {
            if (requested === revision) {
              handlers.rollback(lastSaved)
              handlers.failed(e)
            }
            continue
          }
          if (requested === revision) {
            try {
              await handlers.saved?.(next)
            } catch (e) {
              if (requested === revision) handlers.failed(e) // it was saved: nothing to roll back
            }
          }
        }
      } finally {
        running = null
      }
    })()
    return running
  }
  return Object.assign(set, {
    flush: () => running ?? Promise.resolve(),
    touch: () => {
      revision++
    },
  })
}
