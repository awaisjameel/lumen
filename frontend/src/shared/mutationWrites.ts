import { getActivePinia } from 'pinia'
import type { Pinia } from 'pinia'
import { turns } from './turns'
import type { Turns } from './turns'

export interface MutationLease {
  /** A later explicit edit permanently supersedes this operation for that asset. */
  current: (id: number) => boolean
  release: () => void
}

export interface MutationWrites {
  begin: (ids: readonly number[]) => MutationLease
  take: Turns['take']
  write: <T>(ids: readonly number[], change: () => Promise<T>) => Promise<T>
  /** An explicit entity deletion retires every inverse of this domain. */
  invalidate: () => void
}

export type MutationDomain = 'favourites' | 'date' | 'place' | `album:${number}`

const registries = new WeakMap<Pinia, Map<MutationDomain, MutationWrites>>()

/**
 * Field writes take bounded turns. A new edit invalidates older inverse
 * ownership immediately, even while its write waits for an in-flight unit.
 * Only outstanding writes and live Undo offers retain per-asset tickets.
 */
export function useMutationWrites(): { owner: (domain: MutationDomain) => MutationWrites } {
  const pinia = getActivePinia()
  if (!pinia) throw new Error('Mutation writes need an active library')
  let registry = registries.get(pinia)
  if (!registry) {
    registry = new Map()
    registries.set(pinia, registry)
  }
  const owners = registry
  return {
    owner(domain) {
      let owner = owners.get(domain)
      if (!owner) {
        owner = mutationWrites()
        owners.set(domain, owner)
      }
      return owner
    },
  }
}

function mutationWrites(): MutationWrites {
  const queue = turns()
  const latest = new Map<number, { current: boolean }>()
  function begin(ids: readonly number[]): MutationLease {
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
  const owner: MutationWrites = {
    begin,
    invalidate() {
      for (const ticket of latest.values()) ticket.current = false
      latest.clear()
    },
    take: queue.take,
    write(ids, change) {
      const lease = begin(ids)
      return queue.take(change).finally(lease.release)
    },
  }
  return owner
}
