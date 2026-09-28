import { engine } from '../bridge'
import { defineListStore } from '../shared/listStore'

/** Saved collections with live counts and covers, loaded on demand. */
export const useAlbums = defineListStore('albums', engine.albums)
