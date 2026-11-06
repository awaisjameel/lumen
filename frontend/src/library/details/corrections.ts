import { onBeforeUnmount, ref } from 'vue'
import { engine } from '../../bridge'
import type { AssetDetail } from '../../bridge'
import { errorText, reportError } from '../../shared/errors'
import { useMutationWrites } from '../../shared/mutationWrites'
import { offerBatchUndo } from '../../shared/undo'
import type { UndoResult } from '../../shared/undo'
import { useUndoReconcile } from '../undo'
import { t } from '../../shared/i18n'

/** Camera clock values stay in UTC even when the operating system uses another zone. */
export function dateInput(seconds: number): string {
  return new Date(seconds * 1000).toISOString().slice(0, 19)
}

/** Reject calendar rollovers before converting a user's wall-clock date to seconds. */
export function parseDateInput(value: string): number | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(value)) return
  const full = value.length === 16 ? `${value}:00` : value
  const date = new Date(`${full}Z`)
  if (!Number.isFinite(date.getTime()) || date.getUTCFullYear() < 1800 || date.getUTCFullYear() > 2199) return
  if (date.toISOString().slice(0, 19) !== full) return
  return date.getTime() / 1000
}

/** Atomic forward corrections and independently recoverable, captured per-asset inverses. */
export function useCorrection(field: 'date' | 'place', changed: () => void, close: () => void) {
  const busy = ref(false)
  const error = ref('')
  const owner = useMutationWrites().owner(field)
  const refresh = useUndoReconcile()
  const reconcile = () => refresh(changed)
  let alive = true
  onBeforeUnmount(() => (alive = false))
  async function save<State>(
    title: string,
    targets: readonly number[],
    apply: (current: () => boolean) => Promise<boolean>,
    capture: (asset: AssetDetail) => State,
    restore: (id: number, previous: NoInfer<State>) => Promise<void>,
  ) {
    if (busy.value) return
    const ids = [...new Set(targets)]
    const lease = owner.begin(ids)
    const previous = new Map<number, { state: State }>()
    let retained = false
    const current = () => alive && ids.every(lease.current)
    busy.value = true
    error.value = ''
    try {
      const saved = await owner.take(async () => {
        for (const id of ids) {
          if (!alive || !lease.current(id)) return false
          const asset = await engine.asset(id)
          if (asset.id !== id) throw new Error(t('error.unexpected'))
          previous.set(id, { state: capture(asset) })
        }
        return current() ? apply(current) : false
      })
      if (!saved) return
      const owned = ids.filter(lease.current)
      if (owned.length) {
        retained = true
        offerBatchUndo({
          title,
          undoTitle: t(field === 'date' ? 'undo.dates' : 'undo.places'),
          ids: owned,
          unitSize: 1,
          apply: (part, stopped) =>
            owner.take(async () => {
              const rows: UndoResult[] = []
              for (const id of part) {
                if (stopped()) rows.push({ id, state: 'cancelled', error: '' })
                else if (!lease.current(id)) rows.push({ id, state: 'superseded', error: '' })
                else {
                  const before = previous.get(id)
                  if (!before) throw new Error(t('error.unexpected'))
                  await restore(id, before.state)
                  rows.push({ id, state: 'done', error: '' })
                }
              }
              return rows
            }),
          reconcile,
          release: lease.release,
        })
      }
      const refreshing = reconcile().catch(reportError)
      if (alive) close()
      await refreshing
    } catch (e) {
      if (alive) error.value = errorText(e)
    } finally {
      if (!retained) lease.release()
      if (alive) busy.value = false
    }
  }
  return { busy, error, save }
}
