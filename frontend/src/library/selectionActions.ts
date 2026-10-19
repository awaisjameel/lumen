import { computed, onScopeDispose, ref, shallowRef } from 'vue'
import { BatchAction, engine, Filter } from '../bridge'
import type { BatchOutcome } from '../bridge'
import { errorText, failureText, reportError } from '../shared/errors'
import { formatNumber, t } from '../shared/i18n'
import { notify } from '../shared/notify'
import { showTrashUndo } from '../trash/undo'
import type { useLibrary } from './store'

/** Bounded selection mutations keep completed work and report each failed item. */
export function useSelectionActions(lib: ReturnType<typeof useLibrary>) {
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
    try {
      if (snapshot) await engine.validateSelection(snapshot)
      const previous =
        action === BatchAction.BatchFavourite || action === BatchAction.BatchUnfavourite
          ? await engine.searchHits(ids)
          : []
      const albumMembers = album
        ? new Set((await engine.selection({ filter: Filter.FilterAll, album }, [], false)).ids)
        : new Set<number>()
      for (let i = 0; i < ids.length; i += 200) {
        if (isStopped()) {
          outcomes.value = [
            ...outcomes.value,
            ...ids.slice(i).map((id) => ({ id, code: '', error: '', cancelled: true })),
          ]
          break
        }
        const part = ids.slice(i, i + 200)
        try {
          const result = await engine.applyBatch(part, action, album)
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
        const before = previous.filter((item) => succeeded.includes(item.id))
        notify({
          id: 'library.batch',
          kind: 'success',
          title: t(
            action === BatchAction.BatchUnfavourite
              ? 'library.unfavourited'
              : action === BatchAction.BatchFavourite
                ? 'library.favourited'
                : 'albums.saved',
          ),
          actions: before.length
            ? [
                {
                  label: t('settings.undo'),
                  run: () => {
                    void Promise.all(before.map((item) => engine.setFavourite(item.id, !!item.f)))
                      .then(() => lib.trashChanged())
                      .catch(reportError)
                  },
                },
              ]
            : album && membershipChanged.length
              ? [
                  {
                    label: t('settings.undo'),
                    run: () => {
                      void (async () => {
                        for (let i = 0; i < membershipChanged.length; i += 200) {
                          const outcomes = await engine.applyBatch(
                            membershipChanged.slice(i, i + 200),
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
    } catch (e) {
      reportError(e)
    } finally {
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
