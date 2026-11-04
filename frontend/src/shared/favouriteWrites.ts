import { useMutationWrites } from './mutationWrites'
export type { MutationLease as FavouriteLease } from './mutationWrites'

/** All favourite surfaces share the same per-app mutation field. */
export function useFavouriteWrites() {
  return useMutationWrites().owner('favourites')
}
