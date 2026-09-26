import { engine } from '../bridge'
import { defineListStore } from '../shared/listStore'

/** Things (tags) with their counts and covers, loaded on demand. */
export const useThings = defineListStore('things', engine.things)
