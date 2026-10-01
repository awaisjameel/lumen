import { shallowRef } from 'vue'
import type { BlockKind } from '../bridge'
import { useAlbums } from '../albums/store'
import { useDuplicates } from '../duplicates/store'
import { useLibrary } from '../library/store'
import { usePeople } from '../people/store'
import { useThings } from '../things/store'

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

/**
 * After a block was made, changed, undone or removed: the grid and every
 * list loaded or loading (Things, People, Albums, Duplicates) show what is
 * visible now, so a blocked item never shows again on the way back to its list.
 */
export async function showBlocksChanged(): Promise<void> {
  const lists = [useThings(), usePeople(), useAlbums(), useDuplicates()].filter((s) => s.loaded || s.loading)
  await Promise.all([...lists.map((s) => s.invalidate()), useLibrary().refreshVisibility()])
}

/**
 * Ask which scope to block, confirming before changing the library view.
 * Once saved, everything shown is refreshed (showBlocksChanged): callers only
 * act on what is theirs alone. Resolves whether the block was saved.
 */
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
