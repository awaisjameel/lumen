/** Work that takes turns, and a way for everything else to wait until it is over. */
export interface Turns {
  /**
   * Runs task once every turn taken before it has ended, and resolves or
   * rejects as it does. With none before it, it starts at once.
   */
  take: <T>(task: () => Promise<T>) => Promise<T>
  /**
   * The end of the last turn taken, null when none is running or waiting.
   * `while (turns.pending) await turns.pending` waits for them all, and
   * does not wait at all when there is none.
   */
  readonly pending: Promise<void> | null
}

/**
 * Turns for work that must not overlap itself or the edits around it (a
 * reset of the settings): each waits for the one before, and an edit made
 * meanwhile waits on `pending`, so it is applied to what the work left. A
 * turn that fails still ends: its caller hears why, those waiting only that
 * it is over.
 */
export function turns(): Turns {
  let last: Promise<void> | null = null
  return {
    take(task) {
      const previous = last
      const result = (async () => {
        if (previous) await previous
        return task()
      })()
      const ended: Promise<void> = result
        .then(
          () => undefined,
          () => undefined,
        )
        .finally(() => {
          if (last === ended) last = null
        })
      last = ended
      return result
    },
    get pending() {
      return last
    },
  }
}
