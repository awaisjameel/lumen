import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { BatchAction, engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import { recordNotifications, undo } from '../shared/testing'
import { useMutationWrites } from '../shared/mutationWrites'
import { undoOutcomes } from '../shared/undo'
import { useLibrary } from '../library/store'
import { useAlbumChanges } from './changes'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { applyBatch: vi.fn(), albums: vi.fn(), counts: vi.fn(), deleteAlbum: vi.fn(), selection: vi.fn() },
}))
let notices: ReturnType<typeof recordNotifications>
let members: Map<number, Set<number>>
let refused: Set<number>
beforeEach(() => {
  setActivePinia(createPinia())
  notices = recordNotifications()
  members = new Map()
  refused = new Set()
  vi.mocked(engine.albums).mockResolvedValue([])
  vi.mocked(engine.counts).mockResolvedValue({ photos: 3, videos: 0, favourites: 0, people: 0, trash: 0 })
  vi.mocked(engine.selection).mockReset()
  vi.mocked(engine.deleteAlbum).mockImplementation((id) => {
    members.delete(id)
    return Promise.resolve()
  })
  vi.mocked(engine.applyBatch)
    .mockReset()
    .mockImplementation((ids, action, album = 0) => {
      const current = members.get(album) ?? new Set<number>()
      members.set(album, current)
      return Promise.resolve(
        ids.map((id) => {
          if (refused.has(id)) return { id, code: ErrorCode.NotFound, error: 'Unavailable asset' }
          const include = action === BatchAction.BatchAlbum
          const changed = current.has(id) !== include
          if (include) current.add(id)
          else current.delete(id)
          return { id, code: '', error: '', changed }
        }),
      )
    })
})
afterEach(() => {
  for (const notice of notices.shown) notice.closed?.()
  notices.stop()
  undoOutcomes.value = null
})

it('captures only committed changes without a whole-album read or reversing pre-existing membership', async () => {
  members.set(8, new Set([1]))
  const edits = useAlbumChanges()
  const change = edits.begin(8, [1, 2], true)
  await change.apply([1, 2])
  expect(engine.selection).not.toHaveBeenCalled()
  expect([...change.changed]).toEqual([2])
  expect(edits.offer([change], 'Saved')).toBe(true)
  change.release()
  const job = await undo(notices)
  expect(job.counts).toMatchObject({ done: 1, failed: 0 })
  expect(job.outcomes[0]).toMatchObject({ id: 'album:8:asset:2', label: 'Asset 2 · album 8' })
  expect(members.get(8)).toEqual(new Set([1]))
})

it('reconciles partial inverses and retries only the original failed target after navigation', async () => {
  const edits = useAlbumChanges()
  const change = edits.begin(8, [1, 2], true)
  await change.apply([1, 2])
  edits.offer([change], 'Saved')
  change.release()
  refused.add(2)
  vi.mocked(engine.counts).mockClear()
  const job = await undo(notices)
  expect(job.counts).toMatchObject({ done: 1, failed: 1 })
  expect(engine.counts).toHaveBeenCalledOnce()
  const lib = useLibrary()
  lib.selectVisible([99])
  lib.selected = { day: 'another view', id: 99 }
  refused.clear()
  vi.mocked(engine.applyBatch).mockClear()
  await job.retry()
  expect(engine.applyBatch).toHaveBeenCalledExactlyOnceWith([2], BatchAction.BatchRemoveAlbum, 8)
  expect(job.counts).toMatchObject({ done: 2, failed: 0 })
  expect([...lib.selectedIDs]).toEqual([99])
  expect(lib.selected.id).toBe(99)
})

it('keeps a newer explicit membership confirmation even when it was already stored', async () => {
  const edits = useAlbumChanges()
  const older = edits.begin(8, [1, 2], true)
  await older.apply([1, 2])
  edits.offer([older], 'Saved')
  older.release()
  const newer = edits.begin(8, [1], true)
  await newer.apply([1])
  expect(newer.changed.size).toBe(0)
  newer.release()
  vi.mocked(engine.applyBatch).mockClear()
  const job = await undo(notices)
  expect(job.counts).toMatchObject({ done: 1, superseded: 1 })
  expect(engine.applyBatch).toHaveBeenCalledExactlyOnceWith([2], BatchAction.BatchRemoveAlbum, 8)
  expect(members.get(8)).toEqual(new Set([1]))
})

it('waits for current selection reconciliation before source callbacks, even when its read fails', async () => {
  const lib = useLibrary()
  let fail!: (error: Error) => void
  const reading = new Promise<void>((_resolve, reject) => (fail = reject))
  vi.spyOn(lib, 'reconcileChanges').mockReturnValueOnce(reading)
  const edits = useAlbumChanges()
  const change = edits.begin(8, [1], true)
  await change.apply([1])
  const changed = vi.fn()
  edits.offer([change], 'Saved', changed)
  change.release()
  const pending = undo(notices)
  await vi.waitFor(() => expect(lib.reconcileChanges).toHaveBeenCalledOnce())
  expect(changed).not.toHaveBeenCalled()
  fail(new Error('Current grid unavailable'))
  const job = await pending
  expect(changed).toHaveBeenCalledOnce()
  expect(job.counts.done).toBe(1)
  expect(job.refreshError).toContain('Current grid unavailable')
  vi.mocked(engine.applyBatch).mockClear()
  await job.refresh()
  expect(job.refreshError).toBe('')
  expect(engine.applyBatch).not.toHaveBeenCalled()
})

it('retires an old album inverse before deletion and never edits a replacement with the reused ID', async () => {
  const edits = useAlbumChanges()
  const older = edits.begin(8, [1], true)
  await older.apply([1])
  edits.offer([older], 'Saved')
  older.release()
  await edits.remove(8)
  members.set(8, new Set([1])) // a newly created album reuses SQLite's highest deleted row ID
  vi.mocked(engine.applyBatch).mockClear()
  const job = await undo(notices)
  expect(job.counts).toMatchObject({ superseded: 1, done: 0, failed: 0 })
  expect(engine.applyBatch).not.toHaveBeenCalled()
  expect(members.get(8)).toEqual(new Set([1]))
})

it('distinguishes two albums for the same photo and retries only the refused membership', async () => {
  const edits = useAlbumChanges()
  const first = edits.begin(8, [1], true)
  const second = edits.begin(9, [1], true)
  await first.apply([1])
  await second.apply([1])
  edits.offer([first, second], 'Saved')
  first.release()
  second.release()
  const normal = vi.mocked(engine.applyBatch).getMockImplementation()
  if (!normal) throw new Error('Missing membership engine')
  vi.mocked(engine.applyBatch).mockImplementation((ids, action, album = 0) => {
    if (album === 9) return Promise.reject(new Error('Second album temporarily unavailable'))
    return normal(ids, action, album)
  })
  const job = await undo(notices)
  expect(job.outcomes.map((row) => [row.id, row.state])).toEqual([
    ['album:8:asset:1', 'done'],
    ['album:9:asset:1', 'failed'],
  ])
  vi.mocked(engine.applyBatch).mockImplementation(normal).mockClear()
  await job.retry()
  expect(engine.applyBatch).toHaveBeenCalledExactlyOnceWith([1], BatchAction.BatchRemoveAlbum, 9)
})

it('cancels while waiting for its field turn and resumes the captured remainder', async () => {
  const edits = useAlbumChanges()
  const change = edits.begin(8, [1, 2], true)
  await change.apply([1, 2])
  edits.offer([change], 'Saved')
  change.release()
  let release: () => void = () => undefined
  let started = false
  const pending = useMutationWrites()
    .owner('album:8')
    .take(
      () =>
        new Promise<undefined>((resolve) => {
          started = true
          release = () => resolve(undefined)
        }),
    )
  await vi.waitFor(() => expect(started).toBe(true))
  notices.shown[0]?.actions?.[0]?.run()
  notices.shown
    .flatMap((notice) => notice.actions ?? [])
    .find((action) => action.label === 'View outcomes')
    ?.run()
  const job = undoOutcomes.value
  if (!job) throw new Error('Missing job')
  job.cancel()
  vi.mocked(engine.applyBatch).mockClear()
  release()
  await pending
  await vi.waitFor(() => expect(job.busy).toBe(false))
  expect(engine.applyBatch).not.toHaveBeenCalled()
  expect(job.counts.cancelled).toBe(2)
  await job.resume()
  expect(engine.applyBatch).toHaveBeenCalledExactlyOnceWith([1, 2], BatchAction.BatchRemoveAlbum, 8)
})

it('keeps superseded targets distinct when the actual inverse request is unconfirmed', async () => {
  const edits = useAlbumChanges()
  const older = edits.begin(8, [1, 2], true)
  await older.apply([1, 2])
  edits.offer([older], 'Saved')
  older.release()
  const newer = edits.begin(8, [1], true)
  await newer.apply([1])
  newer.release()
  vi.mocked(engine.applyBatch).mockRejectedValueOnce(new Error('Response lost'))
  const job = await undo(notices)
  expect(job.outcomes.map((row) => [row.id, row.state])).toEqual([
    ['album:8:asset:1', 'superseded'],
    ['album:8:asset:2', 'failed'],
  ])
  vi.mocked(engine.applyBatch).mockClear()
  await job.retry()
  expect(engine.applyBatch).toHaveBeenCalledExactlyOnceWith([2], BatchAction.BatchRemoveAlbum, 8)
})
