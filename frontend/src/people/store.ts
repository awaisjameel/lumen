import { engine } from '../bridge'
import { defineListStore } from '../shared/listStore'

/** People with their photo counts and faces to show, loaded on demand. */
export const usePeople = defineListStore('people', engine.people)
