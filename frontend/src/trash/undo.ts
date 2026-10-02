import { shallowRef } from 'vue'

/** The assets just moved to the Trash, while the Notice still offers to undo it. */
export const trashUndo = shallowRef<number[] | null>(null)
let undoTimer = 0

export function showTrashUndo(ids: number[]): void {
  trashUndo.value = ids
  clearTimeout(undoTimer)
  undoTimer = window.setTimeout(() => (trashUndo.value = null), 8000)
}

export function clearTrashUndo(): void {
  clearTimeout(undoTimer)
  trashUndo.value = null
}
