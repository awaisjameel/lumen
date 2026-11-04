import { BatchAction, engine, ErrorCode } from '../bridge'
import type { BatchOutcome } from '../bridge'
import { useUndoReconcile } from '../library/undo'
import { failureText } from '../shared/errors'
import { useMutationWrites } from '../shared/mutationWrites'
import { attemptBatchUndo, offerBatchUndo, SELECTION_PART } from '../shared/undo'
import type { UndoResult } from '../shared/undo'
import { t } from '../shared/i18n'

export type AlbumOutcome = BatchOutcome & { cancelled?: boolean }
interface AlbumChange {
  album: number
  changed: Set<number>
  current: (id: number) => boolean
  apply: (ids: number[], stopped?: () => boolean) => Promise<AlbumOutcome[]>
  inverse: (ids: number[], stopped: () => boolean) => Promise<UndoResult[]>
  retain: () => void
  release: () => void
  dispose: () => void
}
const targetKey = (album: number, asset: number) => `album:${album}:asset:${asset}`

/** Every membership surface uses the same transactional change capture and inverse ownership. */
export function useAlbumChanges() {
  const writes = useMutationWrites()
  const reconcile = useUndoReconcile()
  function begin(album: number, ids: number[], include: boolean): AlbumChange {
    const owner = writes.owner(`album:${album}`)
    const lease = owner.begin(ids)
    const changed = new Set<number>()
    let retained = false
    return {
      album,
      changed,
      current: lease.current,
      apply: (part, stopped = () => false) =>
        owner.take(async () => {
          if (stopped()) return part.map((id) => ({ id, code: '', error: '', cancelled: true }))
          const eligible = part.filter(lease.current)
          const rows = eligible.length
            ? await engine.applyBatch(eligible, include ? BatchAction.BatchAlbum : BatchAction.BatchRemoveAlbum, album)
            : []
          for (const row of rows) if (!row.error && row.changed) changed.add(row.id)
          const received = new Map(rows.map((row) => [row.id, row]))
          return part.map(
            (id) =>
              received.get(id) ??
              (lease.current(id)
                ? { id, code: '', error: t('error.unexpected') }
                : { id, code: ErrorCode.SelectionChanged, error: failureText(ErrorCode.SelectionChanged, '') }),
          )
        }),
      inverse: (part, stopped) =>
        owner.take(async () => {
          if (stopped()) return part.map((id) => ({ id, state: 'cancelled', error: '' }))
          const eligible = part.filter(lease.current)
          const rows = await attemptBatchUndo(eligible, () =>
            engine.applyBatch(eligible, include ? BatchAction.BatchRemoveAlbum : BatchAction.BatchAlbum, album),
          )
          const submitted = new Set(eligible)
          return [
            ...rows,
            ...part.filter((id) => !submitted.has(id)).map((id) => ({ id, state: 'superseded' as const, error: '' })),
          ]
        }),
      retain: () => {
        retained = true
      },
      release: () => {
        if (!retained) lease.release()
      },
      dispose: lease.release,
    }
  }
  function offer(changes: readonly AlbumChange[], title: string, changed: () => void = () => undefined): boolean {
    const targets = new Map<string, { change: AlbumChange; asset: number }>()
    for (const change of changes)
      for (const asset of change.changed)
        if (change.current(asset)) targets.set(targetKey(change.album, asset), { change, asset })
    if (!targets.size) return false
    const captured = new Set([...targets.values()].map(({ change }) => change))
    for (const change of captured) change.retain()
    offerBatchUndo({
      title,
      undoTitle: t('undo.albums'),
      ids: [...targets.keys()],
      label: (key) => {
        const target = targets.get(key)
        return target ? t('undo.albumTarget', { asset: target.asset, album: target.change.album }) : key
      },
      unitSize: captured.size === 1 ? SELECTION_PART : 1,
      apply: async (part, stopped) => {
        const groups = new Map<AlbumChange, number[]>()
        for (const key of part) {
          const target = targets.get(key)
          if (!target) continue
          const ids = groups.get(target.change) ?? []
          ids.push(target.asset)
          groups.set(target.change, ids)
        }
        const rows: UndoResult<string>[] = []
        for (const [change, ids] of groups) {
          const result = await change.inverse(ids, stopped)
          rows.push(...result.map((row) => ({ ...row, id: targetKey(change.album, row.id) })))
        }
        return rows
      },
      reconcile: () => reconcile(changed),
      release: () => {
        for (const change of captured) change.dispose()
      },
    })
    return true
  }
  return {
    begin,
    created: (album: number, ids: number[]) => {
      const change = begin(album, ids, true)
      for (const id of ids) change.changed.add(id)
      return change
    },
    offer,
    reconcile,
    remove: (album: number) => {
      const owner = writes.owner(`album:${album}`)
      owner.invalidate()
      return owner.take(() => engine.deleteAlbum(album))
    },
  }
}
