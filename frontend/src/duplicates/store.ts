import { defineStore } from 'pinia'
import { ref, shallowRef } from 'vue'
import { engine } from '../bridge'
import type { DuplicateGroup } from '../bridge'
import { coalescedLoad } from '../shared/listStore'

/** Exact copies and similar photos to review, loaded on demand. */
export const useDuplicates = defineStore('duplicates', () => {
  const groups = shallowRef<DuplicateGroup[]>([])
  const loaded = ref(false)
  const load = coalescedLoad(engine.duplicateGroups, (got) => {
    groups.value = got
    loaded.value = true
  })
  return { groups, loaded, load }
})
