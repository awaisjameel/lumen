import { getActivePinia } from 'pinia'
import type { Pinia } from 'pinia'
import { turns } from './turns'
import type { Turns } from './turns'

export interface FavouriteLease {
  /** A later explicit edit permanently supersedes this operation for that asset. */
  current: (id: number) => boolean
  release: () => void
}

interface FavouriteWrites {
  begin: (ids: readonly number[]) => FavouriteLease
  take: Turns['take']
  write: <T>(ids: readonly number[], change: () => Promise<T>) => Promise<T>
}

const owners = new WeakMap<Pinia, FavouriteWrites>()

/**
 * Favourite writes take bounded turns. A new edit invalidates older inverse
 * ownership immediately, even while its write waits for an in-flight unit.
 * Only outstanding writes and live Undo offers retain per-asset tickets.
 */
export function useFavouriteWrites(): FavouriteWrites {
  const pinia = getActivePinia()
  if (!pinia) throw new Error('Favourite writes need an active library')
  const existing = owners.get(pinia)
  if (existing) return existing
  const queue = turns()
  const latest = new Map<number, { current: boolean }>()
  function begin(ids: readonly number[]): FavouriteLease {
    const tickets = new Map<number, { current: boolean }>()
    for (const id of new Set(ids)) {
      const older = latest.get(id)
      if (older) older.current = false
      const ticket = { current: true }
      tickets.set(id, ticket)
      latest.set(id, ticket)
    }
    return {
      current: (id) => tickets.get(id)?.current ?? false,
      release: () => {
        for (const [id, ticket] of tickets) {
          ticket.current = false
          if (latest.get(id) === ticket) latest.delete(id)
        }
        tickets.clear()
      },
    }
  }
  const owner: FavouriteWrites = {
    begin,
    take: queue.take,
    write(ids, change) {
      const lease = begin(ids)
      return queue.take(change).finally(lease.release)
    },
  }
  owners.set(pinia, owner)
  return owner
}
