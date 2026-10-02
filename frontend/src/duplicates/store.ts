import { defineStore } from 'pinia'
import { shallowRef, watch } from 'vue'
import { DuplicateKind, engine } from '../bridge'
import type { DuplicateGroup, DuplicateItem } from '../bridge'
import { defineCollectionStore } from '../shared/listStore'

/** Exact copies and similar photos to review, loaded on demand. */
export const useDuplicates = defineCollectionStore('duplicates', engine.duplicateGroups)

/** What a group would keep, and so what Review would act on. */
export interface Keeper {
  item: DuplicateItem
  /** The file to keep: of an exact group's one photo (the others are recycled); "" in a similar group. */
  path: string
}

/** What confirming a group's review does, for the confirmation to list. */
export interface Review {
  kind: DuplicateKind
  keep: DuplicateItem
  keepPath: string
  /** Similar photos that go to the Trash. */
  trash: DuplicateItem[]
  /** Exact copies' other files, which go to the Recycle Bin. */
  recycle: string[]
}

/** Identifies a choice: the same photo may head an exact group and sit in a similar one. */
function choiceKey(group: DuplicateGroup, item: DuplicateItem): string {
  return `${group.kind}:${item.id}`
}

/**
 * The copy the user picked in each group, falling back to the engine's
 * suggestion (best). A choice belongs to the photo (and, in an exact group,
 * the file) the user picked, so it survives a reload for as long as that
 * photo is still in a group, and is forgotten once it is not.
 */
export const useKeepChoices = defineStore('duplicateChoices', () => {
  const duplicates = useDuplicates()
  // Choice key -> the chosen file ("" in a similar group). Replaced, never mutated.
  const chosen = shallowRef(new Map<string, string>())

  /** The copy the group keeps now, or null for a group without photos. */
  function keeperOf(group: DuplicateGroup): Keeper | null {
    if (group.kind === DuplicateKind.DuplicateExact) {
      const item = group.items[0]
      if (!item) return null
      const picked = chosen.value.get(choiceKey(group, item))
      return { item, path: picked && item.paths.includes(picked) ? picked : item.keep || (item.paths[0] ?? '') }
    }
    const item =
      group.items.find((it) => chosen.value.has(choiceKey(group, it))) ??
      group.items.find((it) => it.best) ??
      group.items[0]
    return item ? { item, path: '' } : null
  }

  /** Keep this photo of a similar group, or this file of an exact group's photo. */
  function choose(group: DuplicateGroup, item: DuplicateItem, path = ''): void {
    const next = new Map(chosen.value)
    for (const it of group.items) next.delete(choiceKey(group, it)) // one choice per group
    next.set(choiceKey(group, item), path)
    chosen.value = next
  }

  /** What Review would do for the group, or null if it holds nothing to remove. */
  function reviewOf(group: DuplicateGroup): Review | null {
    const keeper = keeperOf(group)
    if (!keeper) return null
    const exact = group.kind === DuplicateKind.DuplicateExact
    const review: Review = {
      kind: group.kind,
      keep: keeper.item,
      keepPath: keeper.path,
      trash: exact ? [] : group.items.filter((it) => it.id !== keeper.item.id),
      recycle: exact ? keeper.item.paths.filter((p) => p !== keeper.path) : [],
    }
    return review.trash.length || review.recycle.length ? review : null
  }

  // Forget choices whose photo (or file) has left every group.
  watch(
    () => duplicates.list,
    (groups) => {
      const alive = new Map<string, DuplicateItem>()
      for (const g of groups) for (const it of g.items) alive.set(choiceKey(g, it), it)
      const kept = [...chosen.value].filter(([key, path]) => {
        const item = alive.get(key)
        return item !== undefined && (path === '' || item.paths.includes(path))
      })
      if (kept.length !== chosen.value.size) chosen.value = new Map(kept)
    },
  )

  return { keeperOf, choose, reviewOf }
})
