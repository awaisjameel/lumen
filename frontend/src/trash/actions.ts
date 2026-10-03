import { engine } from '../bridge'
import type { DiskDeletion } from '../bridge'
import type { Sel, useLibrary } from '../library/store'
import { confirm } from '../shared/confirm'
import { reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import { notify } from '../shared/notify'

type Library = ReturnType<typeof useLibrary>

/** The id of the notification that says files could not be recycled. */
export const KEPT_NOTIFICATION = 'trash.kept'

/** Says n files could not be moved to the Recycle Bin and were left where they are. */
export function notifyKept(n: number): void {
  notify({ id: KEPT_NOTIFICATION, kind: 'info', title: t('trash.kept', { n }), duration: 8000 })
}

/** Says which files the Recycle Bin would not take (they were left where they are). */
function reportKept(result: DiskDeletion): void {
  if (result.kept.length) notifyKept(result.kept.length)
}

/** Ask the user to confirm moving files (the engine's own count of them) to the Recycle Bin. */
async function confirmed(ids: number[]): Promise<boolean> {
  let files: number
  try {
    files = await engine.trashFileCount(ids)
  } catch (e) {
    reportError(e)
    return false
  }
  return confirm(t('trash.confirmTitle'), t('trash.confirmFiles', { n: files }), t('trash.confirmOk'))
}

/**
 * Delete the trashed asset at p from disk: after the user confirms (the
 * question says how many files that is), its files go to the Recycle Bin, and
 * the viewer moves on. Nothing is deleted if they decline.
 */
export async function deleteFromDisk(lib: Library, p: Sel): Promise<void> {
  if (!(await confirmed([p.id]))) return
  const done = await lib.removeFromView(p, () => engine.deleteFromDisk([p.id]))
  if (done) reportKept(done.value)
}

/** Delete everything in the Trash from disk, after confirming how many files that is. */
export async function deleteAllFromDisk(lib: Library): Promise<void> {
  if (!(await confirmed([]))) return
  try {
    reportKept(await engine.deleteAllTrashFromDisk())
  } catch (e) {
    reportError(e)
  } finally {
    await lib.trashChanged()
  }
}

/** Take everything out of the Trash. */
export async function restoreAll(lib: Library): Promise<void> {
  try {
    await engine.restoreAllTrash()
  } catch (e) {
    reportError(e)
  } finally {
    await lib.trashChanged()
  }
}
