import { shallowRef } from 'vue'
import type { BlockKind } from '../bridge'

export interface BlockRequest {
  kind: BlockKind
  targetID: number
  path: string
  name: string
  labelOnly: boolean
  photosAllowed: boolean
  answer: (saved: boolean) => void
}

export const blockRequest = shallowRef<BlockRequest | null>(null)

export const blockUndo = shallowRef<{ id: number; name: string } | null>(null)
let undoTimer = 0

export function showBlockUndo(id: number, name: string): void {
  blockUndo.value = { id, name }
  clearTimeout(undoTimer)
  undoTimer = window.setTimeout(() => (blockUndo.value = null), 8000)
}

export function clearBlockUndo(): void {
  clearTimeout(undoTimer)
  blockUndo.value = null
}

/** Ask which scope to block, confirming before changing the library view. */
export function block(
  kind: BlockKind,
  targetID: number,
  name: string,
  labelOnly = true,
  path = '',
  photosAllowed = true,
): Promise<boolean> {
  blockRequest.value?.answer(false)
  return new Promise((resolve) => {
    blockRequest.value = { kind, targetID, path, name, labelOnly, photosAllowed, answer: resolve }
  })
}
