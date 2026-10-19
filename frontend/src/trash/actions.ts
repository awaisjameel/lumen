import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { engine, Filter } from '../bridge'
import type { DiskDeletion, TrashSelection } from '../bridge'
import type { Sel, useLibrary } from '../library/store'
import type { Command } from '../shared/commands'
import { confirm } from '../shared/confirm'
import { reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import { notify } from '../shared/notify'
import { showRestoreUndo } from './undo'

type Library = ReturnType<typeof useLibrary>

/** The id of the notification that says files could not be recycled. */
export const KEPT_NOTIFICATION = 'trash.kept'

/** Says which files could not be moved and how to try again. */
export function notifyKept(n: number, paths: string[] = []): void {
  notify({
    id: KEPT_NOTIFICATION,
    kind: 'info',
    title: t('trash.kept', { n }),
    description: [...paths, t('trash.keptRecovery')].join('\n'),
    duration: 8000,
  })
}

function reportDeletion(result: DiskDeletion): void {
  if (result.kept.length) notifyKept(result.kept.length, result.kept)
  if (result.recycled)
    notify({
      id: 'trash.recycled',
      kind: 'success',
      title: t('trash.recycled', { n: result.recycled }),
      description: t('trash.recycleRecovery'),
    })
}

/** The engine's snapshot is what gets deleted, even if the view changes. */
async function confirmed(ids: number[]): Promise<TrashSelection | undefined> {
  try {
    const sel = await engine.trashSelection(ids)
    if (!sel.assets.length || !sel.files) return undefined
    const ok = await confirm(
      t('trash.confirmTitle'),
      t('trash.confirmFiles', { n: sel.files }),
      t('trash.confirmCount', { n: sel.files }),
      { danger: true },
    )
    return ok ? sel : undefined
  } catch (e) {
    reportError(e)
    return undefined
  }
}

/** Recycles one trashed asset and moves the viewer on after confirmation. */
export async function deleteFromDisk(lib: Library, p: Sel): Promise<void> {
  const sel = await confirmed([p.id])
  if (!sel) return
  const done = await lib.removeFromView(p, () => engine.deleteFromDisk(sel))
  if (done) reportDeletion(done.value)
}

async function recycle(lib: Library, ids: number[]) {
  const sel = await confirmed(ids)
  if (!sel) return
  try {
    reportDeletion(await engine.deleteFromDisk(sel))
    lib.clearSelection()
  } catch (e) {
    reportError(e)
  } finally {
    await lib.trashChanged()
  }
}

/** Recycles all of the Trash, after confirming the disk-file count. */
export async function deleteAllFromDisk(lib: Library): Promise<void> {
  await recycle(lib, [])
}

/** Restores exactly these IDs so Undo never moves unrelated library items. */
export async function restoreSelection(lib: Library, ids: number[]): Promise<void> {
  if (!ids.length) return
  const restored = [...ids]
  try {
    await engine.restoreAssets(restored)
    lib.clearSelection()
    showRestoreUndo(restored, async (undoIDs) => {
      await engine.trashAssets(undoIDs)
      await lib.trashChanged()
    })
  } catch (e) {
    reportError(e)
  } finally {
    await lib.trashChanged()
  }
}

/** Snapshot before restoring, so Undo refers only to what was restored. */
export async function restoreAll(lib: Library): Promise<void> {
  try {
    const sel = await engine.trashSelection([])
    await restoreSelection(lib, sel.assets)
  } catch (e) {
    reportError(e)
  }
}

// The header and selection dock share a lock, including the confirmation,
// so two entry points cannot start overlapping recovery actions.
const useTrashWork = defineStore('trashWork', () => {
  const busy = ref<'restore' | 'recycle' | null>(null)
  return { busy }
})

/** Commands consumed by LibraryView's SelectionDock and registered there. */
export function useTrashActions(lib: Library) {
  const work = useTrashWork()
  const inTrash = () => lib.scope.kind === 'timeline' && lib.scope.view.filter === Filter.FilterTrash
  const reason = computed(() =>
    work.busy
      ? t('trash.busyReason')
      : !lib.timelineLoaded
        ? t('trash.loadingReason')
        : !lib.total
          ? t('trash.emptyReason')
          : undefined,
  )
  async function act(action: 'restore' | 'recycle', all = false) {
    if (reason.value || !inTrash()) return
    const ids = [...lib.selectedIDs]
    if (!all && !ids.length) return
    work.busy = action
    try {
      if (action === 'restore') {
        if (all) await restoreAll(lib)
        else await restoreSelection(lib, ids)
      } else await recycle(lib, all ? [] : ids)
    } finally {
      work.busy = null
    }
  }
  const commands: Command[] = [
    {
      id: 'trash.restore',
      group: 'media',
      title: () => t('trash.restore'),
      icon: 'restore',
      available: () => inTrash() && lib.selectedIDs.size > 0,
      enabled: () => !reason.value,
      reason: () => reason.value,
      run: () => act('restore'),
    },
    {
      id: 'trash.recycle',
      group: 'media',
      title: () => t('trash.deleteFromDisk'),
      icon: 'recycle',
      danger: true,
      available: () => inTrash() && lib.selectedIDs.size > 0,
      enabled: () => !reason.value,
      reason: () => reason.value,
      run: () => act('recycle'),
    },
  ]
  return { work, reason, commands, act }
}
