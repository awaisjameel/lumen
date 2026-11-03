import { computed, onScopeDispose, ref, shallowRef } from 'vue'
import { BatchAction, engine, Filter } from '../bridge'
import type { BatchOutcome } from '../bridge'
import { errorText, failureText, reportError } from '../shared/errors'
import { formatNumber, t } from '../shared/i18n'
import { notify } from '../shared/notify'
import { useFavouriteWrites } from '../shared/favouriteWrites'
import { SELECTION_PART } from '../shared/undo'
import { showTrashUndo } from '../trash/undo'
import type { useLibrary } from './store'
import { favouriteBatch } from './favouriteBatch'

/** Bounded selection mutations keep completed work and report each failed item. */
export function useSelectionActions(lib: ReturnType<typeof useLibrary>) {
  const favouriteWrites = useFavouriteWrites()
  const busy = ref(false)
  const stopped = ref(false)
  const total = ref(0)
  const outcomes = shallowRef<(BatchOutcome & { cancelled?: boolean })[]>([])
  const open = ref(false)
  const lifetime = { active: true }
  let lastAction = BatchAction.BatchFavourite
  let lastAlbum = 0
  onScopeDispose(() => {
    lifetime.active = false
    stopped.value = true
  })
  const failed = computed(() => outcomes.value.filter((row) => row.error && !row.cancelled))
  const done = computed(() => outcomes.value.filter((row) => !row.error && !row.cancelled).map((row) => row.id))
  const cancelled = computed(() => outcomes.value.filter((row) => row.cancelled).length)
  const caption = computed(() =>
    busy.value
      ? t('library.batchWorking', {
          n: formatNumber(outcomes.value.length),
          total: formatNumber(total.value),
          failed: formatNumber(failed.value.length),
        })
      : t('library.batchFinished', {
          n: formatNumber(done.value.length),
          failed: formatNumber(failed.value.length),
          cancelled: formatNumber(cancelled.value),
        }),
  )

  const isStopped = () => stopped.value || !lifetime.active
  async function apply(action: BatchAction, album = 0, retryIDs?: number[]) {
    if (busy.value) return
    const ids = retryIDs ?? [...lib.selectedIDs]
    if (!ids.length) return
    const snapshot = retryIDs ? null : lib.selectionSnapshot
    const previousSelection = new Set(lib.selectedIDs)
    const previousFocus = lib.selected
    lastAction = action
    lastAlbum = album
    busy.value = true
    stopped.value = false
    outcomes.value = []
    total.value = ids.length
    const favourite = action === BatchAction.BatchFavourite || action === BatchAction.BatchUnfavourite
    let favourites: ReturnType<typeof favouriteBatch> | undefined
    try {
      if (snapshot) await engine.validateSelection(snapshot)
      if (favourite) favourites = favouriteBatch(favouriteWrites, ids, action === BatchAction.BatchFavourite)
      const albumMembers =
        album && !favourite
          ? new Set((await engine.selection({ filter: Filter.FilterAll, album }, [], false)).ids)
          : new Set<number>()
      for (let i = 0; i < ids.length; i += SELECTION_PART) {
        if (isStopped()) {
          outcomes.value = [
            ...outcomes.value,
            ...ids.slice(i).map((id) => ({ id, code: '', error: '', cancelled: true })),
          ]
          break
        }
        const part = ids.slice(i, i + SELECTION_PART)
        try {
          const result = favourites
            ? await favourites.apply(part, isStopped)
            : await engine.applyBatch(part, action, album)
          outcomes.value = [...outcomes.value, ...result]
        } catch (e) {
          outcomes.value = [...outcomes.value, ...part.map((id) => ({ id, code: '', error: errorText(e) }))]
        }
      }
      const succeeded = [...done.value]
      if (action === BatchAction.BatchTrash && succeeded.length) {
        lib.clearSelection()
        lib.selected = null
        showTrashUndo(succeeded, async (restored) => {
          await lib.restoreAssets(restored)
          lib.selectVisible([...previousSelection])
          lib.selected = previousFocus
        })
      } else if (succeeded.length) {
        const membershipChanged = succeeded.filter((id) =>
          action === BatchAction.BatchAlbum ? !albumMembers.has(id) : albumMembers.has(id),
        )
        if (!favourites?.offer(() => lib.reconcileChanges())) {
          notify({
            id: 'library.batch',
            kind: 'success',
            title: favourites?.title ?? t('albums.saved'),
            actions:
              album && membershipChanged.length
                ? [
                    {
                      label: t('settings.undo'),
                      run: () => {
                        void (async () => {
                          for (let i = 0; i < membershipChanged.length; i += SELECTION_PART) {
                            const outcomes = await engine.applyBatch(
                              membershipChanged.slice(i, i + SELECTION_PART),
                              action === BatchAction.BatchAlbum ? BatchAction.BatchRemoveAlbum : BatchAction.BatchAlbum,
                              album,
                            )
                            const failure = outcomes.find((row) => row.error)
                            if (failure) throw new Error(failureText(failure.code, failure.error))
                          }
                          await lib.trashChanged()
                        })().catch(reportError)
                      },
                    },
                  ]
                : undefined,
          })
        }
      }
    } catch (e) {
      reportError(e)
    } finally {
      favourites?.release()
      busy.value = false
      await lib.trashChanged()
    }
  }
  return {
    busy,
    outcomes,
    open,
    failed,
    caption,
    total,
    cancel: () => {
      stopped.value = true
    },
    text: (row: BatchOutcome & { cancelled?: boolean }) =>
      row.cancelled
        ? t('files.outcome.cancelled')
        : row.error
          ? failureText(row.code, row.error)
          : t('files.outcome.done'),
    apply,
    retry: () =>
      apply(
        lastAction,
        lastAlbum,
        failed.value.map((row) => row.id),
      ),
  }
}
