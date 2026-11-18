import { Filter } from '../bridge'
import { destination } from '../shared/navigation/contract'
import type { Destination } from '../shared/navigation/contract'
import type { Scope } from './store'

type GridDestination = Extract<Destination, { meta: { view: 'library' } }>
export function isGridDestination(target: Destination | undefined): target is GridDestination {
  return target?.meta.view === 'library'
}
export function mediaOf(target: GridDestination | undefined): Filter {
  if (target?.query.media === 'photos') return Filter.FilterPhotos
  if (target?.query.media === 'videos' || target?.name === 'videos') return Filter.FilterVideos
  return Filter.FilterAll
}
/** Only admitted grid destinations can produce an engine scope. */
export function scopeOf(target: GridDestination | undefined): Scope | undefined {
  if (!target) return
  const media = mediaOf(target)
  if (target.name === 'search')
    return {
      kind: 'search',
      query: target.query.q ?? '',
      media,
      favourites: target.query.favourites,
      from: target.query.from ?? '',
      to: target.query.to ?? '',
      place: target.query.place ?? 0,
    }
  return {
    kind: 'timeline',
    media: media === Filter.FilterAll ? undefined : media,
    view: {
      filter:
        target.name === 'favourites' ? Filter.FilterFavourites : target.name === 'trash' ? Filter.FilterTrash : media,
      tag: target.name === 'thing' ? target.params.id : 0,
      person: target.name === 'person' ? target.params.id : 0,
      album: target.name === 'album' ? target.params.id : 0,
      place: target.name === 'place' ? target.params.id : 0,
      event: target.name === 'event' ? target.params.id : 0,
    },
  }
}
/** Replacing the legacy Videos entry retains every compatible refinement. */
export function withMedia(target: GridDestination, media: Filter) {
  const value = media === Filter.FilterPhotos ? 'photos' : media === Filter.FilterVideos ? 'videos' : undefined
  if (target.name === 'search') return destination({ name: 'search', query: { ...target.query, media: value } })
  if (
    target.name === 'person' ||
    target.name === 'thing' ||
    target.name === 'album' ||
    target.name === 'place' ||
    target.name === 'event'
  )
    return destination({ name: target.name, params: target.params, query: { media: value } })
  return destination({ name: target.name === 'videos' ? 'library' : target.name, query: { media: value } })
}
