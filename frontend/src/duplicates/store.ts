import { engine } from '../bridge'
import { defineCollectionStore } from '../shared/listStore'

/** Exact copies and similar photos to review, loaded on demand. */
export const useDuplicates = defineCollectionStore('duplicates', engine.duplicateGroups)
