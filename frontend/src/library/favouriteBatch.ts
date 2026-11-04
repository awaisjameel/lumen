import { BatchAction, engine, ErrorCode } from '../bridge'
import { errorText, failureText } from '../shared/errors'
import type { useFavouriteWrites } from '../shared/favouriteWrites'
import { t } from '../shared/i18n'
import { attemptBatchUndo, offerBatchUndo } from '../shared/undo'

/** Capture flags beside their writes; the inverse only owns genuinely changed targets. */
export function favouriteBatch(writes: ReturnType<typeof useFavouriteWrites>, ids: number[], favourite: boolean) {
  const lease = writes.begin(ids)
  const changed = new Set<number>()
  const title = t(favourite ? 'library.favourited' : 'library.unfavourited')
  let offered = false

  return {
    title,
    apply: (part: number[], stopped: () => boolean) =>
      writes.take(async () => {
        if (stopped()) return part.map((id) => ({ id, code: '', error: '', cancelled: true }))
        const current = part.filter(lease.current)
        const previous = new Map(
          (current.length ? await engine.searchHits(current) : []).map((item) => [item.id, !!item.f]),
        )
        if (stopped()) return part.map((id) => ({ id, code: '', error: '', cancelled: true }))
        const targets = current.filter((id) => lease.current(id) && previous.has(id))
        const rows = targets.length
          ? await engine.applyBatch(targets, favourite ? BatchAction.BatchFavourite : BatchAction.BatchUnfavourite, 0)
          : []
        const received = new Map(rows.map((row) => [row.id, row]))
        return part.map((id) => {
          const row = received.get(id)
          if (row) {
            if (!row.error && previous.get(id) !== favourite) changed.add(id)
            return row
          }
          const code = lease.current(id) ? ErrorCode.NotFound : ErrorCode.SelectionChanged
          return { id, code, error: failureText(code, '') }
        })
      }),
    offer(reconcile: () => Promise<unknown>): boolean {
      const targets = [...changed].filter(lease.current)
      if (!targets.length) return false
      offered = true
      offerBatchUndo({
        title,
        undoTitle: t('undo.favourites'),
        ids: targets,
        apply: (part, stopped) =>
          writes.take(async () => {
            if (stopped()) return part.map((id) => ({ id, state: 'cancelled', error: '' }))
            const eligible = part.filter(lease.current)
            let previous: Map<number, boolean>
            try {
              previous = new Map(
                (eligible.length ? await engine.searchHits(eligible) : []).map((item) => [item.id, !!item.f]),
              )
            } catch (e) {
              return part.map((id) => ({
                id,
                state: lease.current(id) ? 'failed' : 'superseded',
                error: lease.current(id) ? errorText(e) : '',
              }))
            }
            if (stopped()) return part.map((id) => ({ id, state: 'cancelled', error: '' }))
            const current = eligible.filter(lease.current)
            const owned = new Set(current)
            // A failed response can hide a committed write. Checking the
            // current flag makes retry idempotent without another journal.
            const targets = current.filter((id) => previous.get(id) !== !favourite)
            const rows = await attemptBatchUndo(targets, () =>
              engine.applyBatch(targets, favourite ? BatchAction.BatchUnfavourite : BatchAction.BatchFavourite, 0),
            )
            return [
              ...rows,
              ...current
                .filter((id) => previous.get(id) === !favourite)
                .map((id) => ({ id, state: 'done' as const, error: '' })),
              ...part.filter((id) => !owned.has(id)).map((id) => ({ id, state: 'superseded' as const, error: '' })),
            ]
          }),
        reconcile,
        release: lease.release,
      })
      return true
    },
    release() {
      if (!offered) lease.release()
    },
  }
}
