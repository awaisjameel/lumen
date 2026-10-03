import { getActivePinia } from 'pinia'
import type { Pinia } from 'pinia'
import { shallowRef } from 'vue'
import { engine } from '../bridge'
import type { BlockKind } from '../bridge'
import { reportError } from './errors'
import { t } from './i18n'
import { notify } from './notify'

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

/** The id of the notification that offers to undo a block. */
export const BLOCK_UNDO_NOTIFICATION = 'block.undo'

/** Says a block was saved, and offers for a few seconds to undo it. */
export function showBlockUndo(id: number, name: string): void {
  notify({
    id: BLOCK_UNDO_NOTIFICATION,
    kind: 'success',
    title: t('blocked.saved', { name }),
    duration: 8000,
    actions: [{ label: t('blocked.undo'), run: () => void undoBlock(id) }],
  })
}

async function undoBlock(id: number): Promise<void> {
  try {
    await engine.undoBlock(id)
    await showBlocksChanged()
  } catch (e) {
    reportError(e)
  }
}

// What showBlocksChanged refreshes, per app (Pinia instance): each store
// registers itself when it is created, so this shared code needs no
// knowledge of the features whose lists it refreshes.
const refreshers = new WeakMap<Pinia, Set<() => Promise<unknown>>>()

/**
 * Has showBlocksChanged run refresh, for the app whose store is being
 * created: call it from a store's setup. Returns the function that
 * unregisters it, for the store's disposal.
 */
export function onBlocksChanged(refresh: () => Promise<unknown>): () => void {
  const pinia = getActivePinia()
  if (!pinia) return () => undefined // not in a store's setup: nothing to refresh
  let set = refreshers.get(pinia)
  if (!set) refreshers.set(pinia, (set = new Set()))
  set.add(refresh)
  return () => set.delete(refresh)
}

/**
 * After a block was made, changed, undone or removed: the grid and every
 * list (Things, People, Albums, Duplicates…) show what is visible now, so a
 * blocked item never shows again on the way back to its list. A list that
 * was never loaded is not read.
 */
export async function showBlocksChanged(): Promise<void> {
  const pinia = getActivePinia()
  const refresh = pinia ? [...(refreshers.get(pinia) ?? [])] : []
  await Promise.all(refresh.map((r) => r()))
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
