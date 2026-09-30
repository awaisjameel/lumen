import { engine, Kind } from '../bridge'
import type { AlbumRule } from '../bridge'
import { defineListStore } from '../shared/listStore'

/** Saved collections with live counts and covers, loaded on demand. */
export const useAlbums = defineListStore('albums', engine.albums)

/** A rule that matches everything: a manual album's, or a new smart album's to start from. */
export function emptyRule(): AlbumRule {
  return { kind: Kind.$zero, favourites: false, from: '', to: '', tag: 0, person: 0 }
}
