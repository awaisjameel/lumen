import { engine } from '../bridge'
import type { DiskDeletion } from '../bridge'
import type { Sel, useLibrary } from '../library/store'
import { confirm } from '../shared/confirm'
import { notice, reportError } from '../shared/errors'
import { t } from '../shared/i18n'

type Library = ReturnType<typeof useLibrary>

/** Says which files the Recycle Bin would not take (they were left where they are). */
function reportKept(result: DiskDeletion): void {
  if (result.kept.length) notice.value = t('trash.kept', { n: result.kept.length })
}

/**
 * Delete the trashed asset at p from disk: after the user confirms (files is
 * how many files that is), its files go to the Recycle Bin, and the viewer
 * moves on. Nothing is called if they decline.
 */
export async function deleteFromDisk(lib: Library, p: Sel, files: number): Promise<void> {
  if (!(await confirm(t('trash.confirmTitle'), t('trash.confirmFiles', { n: files }), t('trash.confirmOk')))) return
  const done = await lib.removeFromView(p, () => engine.deleteFromDisk([p.id]))
  if (done) reportKept(done.value)
}

/** Delete everything in the Trash (count items) from disk, after confirming. */
export async function deleteAllFromDisk(lib: Library, count: number): Promise<void> {
  if (!(await confirm(t('trash.confirmTitle'), t('trash.confirmAll', { n: count }), t('trash.confirmOk')))) return
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
